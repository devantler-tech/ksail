export type ClassValue = string | false | null | undefined;

// cx joins truthy class fragments into a single className string.
export function cx(...values: ClassValue[]): string {
  return values.filter(Boolean).join(" ");
}
