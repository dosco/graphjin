/**
 * Executable identity shipped by the OpenNeko agent image.
 *
 * OpenShell binds model egress to /proc/<pid>/exe, so this is part of the
 * vendored runtime contract, not an installation setting. Ax Harness calls
 * the model itself, so model egress binds to this binary.
 */
export const AX_HARNESS_BINARY = "/usr/local/bin/ax-harness";
