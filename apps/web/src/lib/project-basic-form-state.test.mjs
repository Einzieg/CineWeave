import assert from "node:assert/strict";
import test from "node:test";

import {
  applyProjectBasicSaveFailure,
  applyProjectBasicSaveSuccess,
  beginProjectBasicSubmission,
  createProjectBasicFormState,
  editProjectBasicField,
  projectBasicValues,
  synchronizeProjectBasicSnapshot,
} from "./project-basic-form-state.ts";

const base = { name: "A", description: "old", revision: 1 };

test("an edit made while save A is pending remains dirty after A succeeds", () => {
  let state = editProjectBasicField(createProjectBasicFormState(base), "name", "submitted A");
  const started = beginProjectBasicSubmission(state, "save-a");
  assert.ok(started);
  state = editProjectBasicField(started.state, "name", "typed B");

  state = applyProjectBasicSaveSuccess(state, started.submission, {
    name: "submitted A",
    description: "old",
    revision: 2,
  });

  assert.deepEqual(projectBasicValues(state), { name: "typed B", description: "old" });
  assert.deepEqual(state.dirtyFields, ["name"]);
  assert.equal(state.inFlightSubmission, undefined);
});

test("a successful save clears fields that were not edited again", () => {
  const edited = editProjectBasicField(createProjectBasicFormState(base), "description", "new");
  const started = beginProjectBasicSubmission(edited, "save-a");
  assert.ok(started);

  const state = applyProjectBasicSaveSuccess(started.state, started.submission, {
    name: "A",
    description: "new",
    revision: 2,
  });

  assert.deepEqual(projectBasicValues(state), { name: "A", description: "new" });
  assert.deepEqual(state.dirtyFields, []);
});

test("a revision conflict releases the submission without clearing the draft", () => {
  const edited = editProjectBasicField(createProjectBasicFormState(base), "name", "local");
  const started = beginProjectBasicSubmission(edited, "save-a");
  assert.ok(started);

  const failed = applyProjectBasicSaveFailure(started.state, started.submission);
  const synchronized = synchronizeProjectBasicSnapshot(failed, {
    name: "remote",
    description: "old",
    revision: 2,
  });

  assert.deepEqual(projectBasicValues(synchronized), { name: "local", description: "old" });
  assert.deepEqual(synchronized.dirtyFields, ["name"]);
});

test("older out-of-order responses cannot roll back a newer confirmed base", () => {
  let state = createProjectBasicFormState({ name: "newer", description: "v3", revision: 3 });
  state = editProjectBasicField(state, "name", "latest input");
  const staleSubmission = {
    clientMutationId: "save-a",
    baseRevision: 1,
    values: { name: "older", description: "v2" },
  };

  state = applyProjectBasicSaveSuccess(state, staleSubmission, {
    name: "older",
    description: "v2",
    revision: 2,
  });

  assert.equal(state.baseSnapshot.revision, 3);
  assert.deepEqual(projectBasicValues(state), { name: "latest input", description: "v3" });
  assert.deepEqual(state.dirtyFields, ["name"]);
});

test("a second submission is serialized while one is in flight", () => {
  const edited = editProjectBasicField(createProjectBasicFormState(base), "name", "first");
  const started = beginProjectBasicSubmission(edited, "save-a");
  assert.ok(started);
  assert.equal(beginProjectBasicSubmission(started.state, "save-b"), null);
});

test("a production configuration revision update does not overwrite an independent basic draft", () => {
  let state = editProjectBasicField(createProjectBasicFormState(base), "description", "local basic edit");
  state = synchronizeProjectBasicSnapshot(state, {
    name: "A",
    description: "old",
    revision: 2,
  });

  assert.deepEqual(projectBasicValues(state), { name: "A", description: "local basic edit" });
  assert.deepEqual(state.dirtyFields, ["description"]);
  const started = beginProjectBasicSubmission(state, "after-production-rebuild");
  assert.equal(started?.submission.baseRevision, 2);
});
