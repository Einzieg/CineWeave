"use client";

import { useMemo, useState } from "react";
import { Loader2, Plus, UsersRound } from "lucide-react";
import { toast } from "sonner";
import { ErrorPanel } from "@/components/shared/error-panel";
import { StatusBadge } from "@/components/shared/status-badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { qk } from "@/lib/query/keys";
import { useApiMutation, useApiQuery, useInvalidateKeys } from "@/lib/query/use-api";
import type { Team, TeamImpact } from "@/lib/types";

export function TeamsPanel({ canManage }: { canManage: boolean }) {
  const [createOpen, setCreateOpen] = useState(false);
  const [selectedTeam, setSelectedTeam] = useState<Team | null>(null);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [memberId, setMemberId] = useState("");
  const [pendingStatus, setPendingStatus] = useState<{ team: Team; impact: TeamImpact } | null>(null);
  const invalidateKeys = useInvalidateKeys();
  const teams = useApiQuery({ key: qk.teams(), queryFn: (session) => studioApi.listTeams(session).then((response) => response.items) });
  const members = useApiQuery({
    key: qk.organizationMembers("", "active", 1),
    enabled: canManage,
    queryFn: (session) => studioApi.listOrganizationMembers(session, session.organizationId, { status: "active", page: 1, pageSize: 100 }),
  });
  const teamMembers = useApiQuery({
    key: qk.teamMembers(selectedTeam?.id ?? "none"),
    enabled: Boolean(selectedTeam),
    queryFn: (session) => studioApi.listTeamMembers(session, selectedTeam!.id).then((response) => response.items),
  });
  const createMutation = useApiMutation({
    mutationFn: (session, values: { name: string; description: string }) => studioApi.createTeam(session, values),
    onSuccess: () => {
      toast.success("团队已创建");
      setCreateOpen(false);
      setName("");
      setDescription("");
      invalidateKeys([qk.teams()]);
    },
  });
  const updateMutation = useApiMutation({
    mutationFn: (session, values: { teamId: string; body: { name?: string; description?: string; status?: string } }) => studioApi.updateTeam(session, values.teamId, values.body),
    onSuccess: (updated) => {
      toast.success(updated.status === "disabled" ? "团队已停用" : "团队已更新");
      setSelectedTeam(updated);
      setName(updated.name);
      setDescription(updated.description ?? "");
      setPendingStatus(null);
      invalidateKeys([qk.teams(), qk.teamImpact(updated.id)]);
    },
  });
  const impactMutation = useApiMutation({
    mutationFn: (session, team: Team) => studioApi.getTeamImpact(session, team.id),
    onSuccess: (impact, team) => setPendingStatus({ team, impact }),
  });
  const addMemberMutation = useApiMutation({
    mutationFn: (session, values: { teamId: string; userId: string }) => studioApi.addTeamMember(session, values.teamId, values.userId),
    onSuccess: (_, values) => {
      toast.success("团队成员已添加");
      setMemberId("");
      invalidateKeys([qk.teams(), qk.teamMembers(values.teamId), qk.organizationMembers("", "active", 1)]);
    },
  });
  const removeMemberMutation = useApiMutation({
    mutationFn: (session, values: { teamId: string; userId: string }) => studioApi.removeTeamMember(session, values.teamId, values.userId),
    onSuccess: (_, values) => {
      toast.success("团队成员已移除");
      invalidateKeys([qk.teams(), qk.teamMembers(values.teamId), qk.organizationMembers("", "active", 1)]);
    },
  });

  const availableMembers = useMemo(() => {
    const assigned = new Set(teamMembers.data?.filter((item) => item.status === "active").map((item) => item.userId));
    return members.data?.items.filter((member) => !assigned.has(member.user.id)) ?? [];
  }, [members.data, teamMembers.data]);

  function openTeam(team: Team) {
    setSelectedTeam(team);
    setName(team.name);
    setDescription(team.description ?? "");
  }

  return (
    <div className="overflow-hidden rounded-xl border bg-card">
      <div className="flex items-center justify-between gap-4 border-b px-4 py-4">
        <div><h2 className="text-sm font-semibold">团队</h2><p className="mt-1 text-xs text-muted-foreground">集中维护成员，并以团队作为角色授权主体。</p></div>
        {canManage ? <Dialog open={createOpen} onOpenChange={(open) => { setCreateOpen(open); if (open) { setName(""); setDescription(""); } }}>
          <DialogTrigger asChild><Button size="sm"><Plus className="mr-2 h-4 w-4" />创建团队</Button></DialogTrigger>
          <DialogContent>
            <DialogHeader><DialogTitle>创建团队</DialogTitle><DialogDescription>创建后可添加成员并绑定资源角色。</DialogDescription></DialogHeader>
            <TeamFields name={name} description={description} onNameChange={setName} onDescriptionChange={setDescription} />
            {createMutation.error ? <ErrorPanel message={errorMessage(createMutation.error)} /> : null}
            <DialogFooter><Button variant="outline" onClick={() => setCreateOpen(false)}>取消</Button><Button disabled={!name.trim() || createMutation.isPending} onClick={() => createMutation.mutate({ name: name.trim(), description: description.trim() })}>{createMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}创建</Button></DialogFooter>
          </DialogContent>
        </Dialog> : null}
      </div>

      {teams.isLoading ? <div className="grid gap-2 p-4"><Skeleton className="h-14" /><Skeleton className="h-14" /></div> : teams.error ? <div className="p-4"><ErrorPanel message={errorMessage(teams.error)} /></div> : teams.data?.length ? (
        <Table>
          <TableHeader><TableRow><TableHead>团队</TableHead><TableHead>状态</TableHead><TableHead>成员</TableHead><TableHead>有效绑定</TableHead><TableHead className="w-24 text-right">操作</TableHead></TableRow></TableHeader>
          <TableBody>{teams.data.map((team) => <TableRow key={team.id}><TableCell><button type="button" className="text-left" onClick={() => openTeam(team)}><span className="block font-medium">{team.name}</span><span className="block text-xs text-muted-foreground">{team.description || "无说明"}</span></button></TableCell><TableCell><StatusBadge status={team.status} /></TableCell><TableCell>{team.memberCount}</TableCell><TableCell>{team.bindingCount}</TableCell><TableCell className="text-right"><Button size="sm" variant="ghost" onClick={() => openTeam(team)}>{canManage ? "管理" : "查看"}</Button></TableCell></TableRow>)}</TableBody>
        </Table>
      ) : <div className="grid place-items-center px-4 py-16 text-center"><UsersRound className="h-8 w-8 text-muted-foreground/60" /><p className="mt-3 text-sm font-medium">尚未创建团队</p></div>}

      <Dialog open={Boolean(selectedTeam)} onOpenChange={(open) => { if (!open) setSelectedTeam(null); }}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader><DialogTitle>{canManage ? "管理团队" : "团队详情"}</DialogTitle><DialogDescription>{canManage ? "编辑团队资料和成员。停用团队后，其角色绑定会立即停止生效。" : "查看团队资料和当前成员。"}</DialogDescription></DialogHeader>
          {selectedTeam ? <div className="grid gap-5">
            <TeamFields name={name} description={description} disabled={!canManage} onNameChange={setName} onDescriptionChange={setDescription} />
            {canManage ? <div className="flex flex-wrap gap-2"><Button size="sm" disabled={!name.trim() || updateMutation.isPending} onClick={() => updateMutation.mutate({ teamId: selectedTeam.id, body: { name: name.trim(), description: description.trim() } })}>保存资料</Button><Button size="sm" variant="outline" disabled={impactMutation.isPending || updateMutation.isPending} onClick={() => impactMutation.mutate(selectedTeam)}>{impactMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}{selectedTeam.status === "active" ? "停用团队" : "恢复团队"}</Button></div> : null}
            <div>
              <div className="mb-2 flex items-center justify-between"><p className="text-xs font-medium text-muted-foreground">团队成员</p><span className="text-xs text-muted-foreground">{teamMembers.data?.filter((item) => item.status === "active").length ?? 0} 人</span></div>
              {canManage ? <div className="mb-3 flex gap-2"><Select value={memberId} onValueChange={setMemberId}><SelectTrigger className="flex-1"><SelectValue placeholder="选择组织成员" /></SelectTrigger><SelectContent>{availableMembers.map((member) => <SelectItem key={member.user.id} value={member.user.id}>{member.user.displayName || member.user.username || member.user.email}</SelectItem>)}</SelectContent></Select><Button variant="outline" disabled={!memberId || addMemberMutation.isPending || selectedTeam.status !== "active"} onClick={() => addMemberMutation.mutate({ teamId: selectedTeam.id, userId: memberId })}>添加</Button></div> : null}
              {teamMembers.isLoading ? <Skeleton className="h-24" /> : <div className="divide-y rounded-lg border">{teamMembers.data?.filter((item) => item.status === "active").map((member) => <div key={member.userId} className="flex items-center justify-between gap-3 px-3 py-2"><div className="min-w-0"><p className="truncate text-sm font-medium">{member.user.displayName || member.user.username || member.user.email}</p><p className="truncate text-xs text-muted-foreground">{member.user.email}</p></div>{canManage ? <Button size="sm" variant="ghost" className="text-destructive hover:text-destructive" disabled={removeMemberMutation.isPending} onClick={() => removeMemberMutation.mutate({ teamId: selectedTeam.id, userId: member.userId })}>移除</Button> : null}</div>)}{teamMembers.data?.every((item) => item.status !== "active") ? <p className="px-3 py-5 text-center text-xs text-muted-foreground">暂无团队成员</p> : null}</div>}
            </div>
            {updateMutation.error || impactMutation.error || addMemberMutation.error || removeMemberMutation.error ? <ErrorPanel message={errorMessage(updateMutation.error || impactMutation.error || addMemberMutation.error || removeMemberMutation.error)} /> : null}
          </div> : null}
        </DialogContent>
      </Dialog>

      <AlertDialog open={Boolean(pendingStatus)} onOpenChange={(open) => { if (!open) setPendingStatus(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader><AlertDialogTitle>{pendingStatus?.team.status === "active" ? "停用团队" : "恢复团队"}</AlertDialogTitle><AlertDialogDescription>{pendingStatus?.team.status === "active" ? `停用后，${pendingStatus.impact.activeMemberCount} 名团队成员将立即失去来自该团队的 ${pendingStatus.impact.activeBindingCount} 项有效角色授权；成员关系与绑定记录会保留。` : `恢复后，保留的 ${pendingStatus?.impact.activeBindingCount ?? 0} 项角色绑定会重新生效。`}</AlertDialogDescription></AlertDialogHeader>
          <AlertDialogFooter><AlertDialogCancel>取消</AlertDialogCancel><AlertDialogAction disabled={updateMutation.isPending} onClick={() => pendingStatus && updateMutation.mutate({ teamId: pendingStatus.team.id, body: { status: pendingStatus.team.status === "active" ? "disabled" : "active" } })}>{updateMutation.isPending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}确认</AlertDialogAction></AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function TeamFields({ name, description, disabled = false, onNameChange, onDescriptionChange }: { name: string; description: string; disabled?: boolean; onNameChange: (value: string) => void; onDescriptionChange: (value: string) => void }) {
  return <div className="grid gap-3"><div className="grid gap-2"><Label htmlFor="team-editor-name">名称</Label><Input id="team-editor-name" value={name} disabled={disabled} onChange={(event) => onNameChange(event.target.value)} /></div><div className="grid gap-2"><Label htmlFor="team-editor-description">说明</Label><Textarea id="team-editor-description" rows={3} value={description} disabled={disabled} onChange={(event) => onDescriptionChange(event.target.value)} /></div></div>;
}

function errorMessage(cause: unknown) {
  return cause instanceof StudioApiError ? cause.message : "团队操作失败，请稍后重试。";
}
