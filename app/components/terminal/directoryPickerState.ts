export type DirectoryPickerEntry = { name: string; path: string };

export type DirectoryPickerViewState = {
  currentPath: string;
  entries: DirectoryPickerEntry[];
  loading: boolean;
  error: string | null;
};

export function createIdleDirectoryPickerState(): DirectoryPickerViewState {
  return {
    currentPath: "",
    entries: [],
    loading: false,
    error: null,
  };
}

export function beginDirectoryLoad(
  state: DirectoryPickerViewState,
): DirectoryPickerViewState {
  return {
    ...state,
    loading: true,
    error: null,
  };
}

export function completeDirectoryLoad(
  state: DirectoryPickerViewState,
  result: { path: string; entries: DirectoryPickerEntry[] },
): DirectoryPickerViewState {
  return {
    ...state,
    currentPath: result.path,
    entries: result.entries,
    loading: false,
    error: null,
  };
}

export function failDirectoryLoad(
  state: DirectoryPickerViewState,
  message: string,
): DirectoryPickerViewState {
  return {
    ...state,
    loading: false,
    error: message,
  };
}

/** Parent of a daemon-host absolute path. Remote list_dir paths, not device FS. */
export function parentDirectoryPath(currentPath: string): string {
  const trimmed = currentPath.replace(/\/+$/, "");
  if (!trimmed || trimmed === "/") {
    return "/";
  }
  const parent = trimmed.replace(/\/[^/]+$/, "");
  return parent || "/";
}

/** Component-local request epoch: bump to invalidate in-flight listDir results. */
export function nextDirectoryListEpoch(current: number): number {
  return current + 1;
}

export function shouldApplyDirectoryListResult(
  requestEpoch: number,
  currentEpoch: number,
): boolean {
  return requestEpoch === currentEpoch;
}
