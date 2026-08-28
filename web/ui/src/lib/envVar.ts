const envVarNamePattern = /^[A-Za-z_][A-Za-z0-9_]*$/;

// envVarError validates an environment-variable name override. A blank name resets to the
// conventional provider default and is therefore valid.
export function envVarError(name: string): string | null {
  if (name.trim() === "") {
    return null;
  }
  if (!envVarNamePattern.test(name)) {
    return "Letters, digits and underscore only; cannot start with a digit.";
  }
  return null;
}
