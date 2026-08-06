export type WorkflowEpisodeProgress = {
  workflowType: string;
  episodeIndex: number;
  episodeTotal: number;
  batchIndex: number;
  batchTotal: number;
  totalItems: number;
};

export function workflowEpisodeProgressLabel(progress: WorkflowEpisodeProgress): string {
  if (progress.episodeIndex <= 0) {
    return "";
  }

  if (progress.workflowType === "source_to_script") {
    const targetTotal = progress.batchTotal > 0 ? progress.batchTotal : progress.totalItems;
    if (targetTotal === 1) {
      return `仅生成第 ${progress.episodeIndex} 集`;
    }
    if (targetTotal > 1) {
      const targetIndex = progress.batchIndex > 0 ? progress.batchIndex : 1;
      return `本次 ${targetIndex}/${targetTotal} · 全书第 ${progress.episodeIndex} 集`;
    }
    return `全书第 ${progress.episodeIndex} 集`;
  }

  if (progress.episodeTotal > 0) {
    return `第 ${progress.episodeIndex}/${progress.episodeTotal} 集`;
  }
  return `第 ${progress.episodeIndex} 集`;
}
