/** Client-side mirror of ``octop.infra.users.password.validate_password_policy``. */

export const MIN_PASSWORD_LENGTH = 6;

export type PasswordPolicyIssue = "same_as_old" | "too_short";

/** Return the first policy violation, or null when acceptable. */
export function passwordPolicyIssue(
  password: string,
  oldPassword?: string,
): PasswordPolicyIssue | null {
  if (oldPassword !== undefined && password === oldPassword) {
    return "same_as_old";
  }
  if (password.length < MIN_PASSWORD_LENGTH) {
    return "too_short";
  }
  return null;
}
