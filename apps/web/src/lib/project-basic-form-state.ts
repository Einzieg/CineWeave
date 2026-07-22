export type ProjectBasicField = "name" | "description";

export type ProjectBasicValues = Record<ProjectBasicField, string>;

export type ProjectBasicSnapshot = ProjectBasicValues & {
  revision: number;
};

export type ProjectBasicSubmission = {
  clientMutationId: string;
  baseRevision: number;
  values: ProjectBasicValues;
};

export type ProjectBasicFormState = {
  baseSnapshot: ProjectBasicSnapshot;
  draft: Partial<ProjectBasicValues>;
  dirtyFields: ProjectBasicField[];
  inFlightSubmission?: ProjectBasicSubmission;
};

const projectBasicFields: ProjectBasicField[] = ["name", "description"];

export function createProjectBasicFormState(baseSnapshot: ProjectBasicSnapshot): ProjectBasicFormState {
  return {
    baseSnapshot,
    draft: {},
    dirtyFields: [],
  };
}

export function projectBasicValues(state: ProjectBasicFormState): ProjectBasicValues {
  return {
    name: state.draft.name ?? state.baseSnapshot.name,
    description: state.draft.description ?? state.baseSnapshot.description,
  };
}

export function editProjectBasicField(
  state: ProjectBasicFormState,
  field: ProjectBasicField,
  value: string,
): ProjectBasicFormState {
  const draft = { ...state.draft };
  const dirtyFields = new Set(state.dirtyFields);
  if (value === state.baseSnapshot[field]) {
    delete draft[field];
    dirtyFields.delete(field);
  } else {
    draft[field] = value;
    dirtyFields.add(field);
  }
  return { ...state, draft, dirtyFields: projectBasicFields.filter((item) => dirtyFields.has(item)) };
}

export function beginProjectBasicSubmission(
  state: ProjectBasicFormState,
  clientMutationId: string,
): { state: ProjectBasicFormState; submission: ProjectBasicSubmission } | null {
  if (state.inFlightSubmission || state.dirtyFields.length === 0) {
    return null;
  }
  const submission: ProjectBasicSubmission = {
    clientMutationId,
    baseRevision: state.baseSnapshot.revision,
    values: projectBasicValues(state),
  };
  return {
    state: { ...state, inFlightSubmission: submission },
    submission,
  };
}

export function applyProjectBasicSaveSuccess(
  state: ProjectBasicFormState,
  submission: ProjectBasicSubmission,
  confirmedSnapshot: ProjectBasicSnapshot,
): ProjectBasicFormState {
  const currentValues = projectBasicValues(state);
  const baseSnapshot = confirmedSnapshot.revision >= state.baseSnapshot.revision
    ? confirmedSnapshot
    : state.baseSnapshot;
  const draft: Partial<ProjectBasicValues> = {};
  const dirtyFields: ProjectBasicField[] = [];

  for (const field of projectBasicFields) {
    const changedAfterSubmission = currentValues[field] !== submission.values[field];
    if (changedAfterSubmission && currentValues[field] !== baseSnapshot[field]) {
      draft[field] = currentValues[field];
      dirtyFields.push(field);
    }
  }

  return {
    baseSnapshot,
    draft,
    dirtyFields,
    inFlightSubmission: state.inFlightSubmission?.clientMutationId === submission.clientMutationId
      ? undefined
      : state.inFlightSubmission,
  };
}

export function applyProjectBasicSaveFailure(
  state: ProjectBasicFormState,
  submission: ProjectBasicSubmission,
): ProjectBasicFormState {
  if (state.inFlightSubmission?.clientMutationId !== submission.clientMutationId) {
    return state;
  }
  return { ...state, inFlightSubmission: undefined };
}

export function synchronizeProjectBasicSnapshot(
  state: ProjectBasicFormState | null,
  nextSnapshot: ProjectBasicSnapshot,
): ProjectBasicFormState {
  if (!state) {
    return createProjectBasicFormState(nextSnapshot);
  }
  if (nextSnapshot.revision < state.baseSnapshot.revision) {
    return state;
  }

  const currentValues = projectBasicValues(state);
  const draft: Partial<ProjectBasicValues> = {};
  const dirtyFields: ProjectBasicField[] = [];
  for (const field of projectBasicFields) {
    if (currentValues[field] !== nextSnapshot[field]) {
      draft[field] = currentValues[field];
      dirtyFields.push(field);
    }
  }
  return { ...state, baseSnapshot: nextSnapshot, draft, dirtyFields };
}
