/**
 * Executable identity shipped by the OpenNeko agent image.
 *
 * OpenShell binds model egress to /proc/<pid>/exe. Ax Harness calls the model
 * itself, so model egress binds to this binary.
 */
export const AX_HARNESS_BINARY = "/usr/local/bin/ax-harness";
