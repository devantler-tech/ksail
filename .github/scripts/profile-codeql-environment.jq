# One definition for pair validation and outcome-independent cohort selection.
def profile_environment:
  {sourceSha,workflowRunId,workflowRunAttempt,architecture,platform,runnerImageOS,runnerImageVersion,
    kernelRelease,codeqlVersion,goVersion,goHeapLimit,extractorSha256,runnerPackagesSha256,processorModelSha256,
    physicalMemoryBytes,logicalProcessors,cgroupLimits,expectedProjects,
    originalSwapBytes:.swapPreparation.originalSwapBytes};
