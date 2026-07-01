export type ExternalUsageSelectionState = {
  selectedUserId: number | null
  importTargetUserId: number | null
}

export function syncExternalUsageSelection(
  userId: number | null
): ExternalUsageSelectionState {
  return {
    selectedUserId: userId,
    importTargetUserId: userId,
  }
}

export function initializeExternalUsageSelection(
  current: ExternalUsageSelectionState,
  candidateUserIds: number[]
): ExternalUsageSelectionState {
  const fallbackUserId = candidateUserIds[0] ?? null
  const selectedUserId = current.selectedUserId ?? fallbackUserId
  return {
    selectedUserId,
    importTargetUserId:
      current.importTargetUserId ?? current.selectedUserId ?? fallbackUserId,
  }
}

export function shouldLoadExternalUsageData(
  isAdmin: boolean,
  selectedUserId: number | null
): boolean {
  return !isAdmin || selectedUserId != null
}

export function canRequestExternalUsageInstallerCommand(params: {
  isAdmin: boolean
  currentUserId: number | null | undefined
  selectedUserId: number | null
}): boolean {
  if (!params.isAdmin) {
    return true
  }
  return params.selectedUserId != null && params.selectedUserId === params.currentUserId
}
