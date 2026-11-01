import { describe, expect, it } from "vitest";
import { profileBuildFailureMessage } from "@/lib/onboarding-failure";

describe("profileBuildFailureMessage", () => {
  it("classifies an agent budget timeout without claiming the runtime vanished", () => {
    const message = profileBuildFailureMessage(
      "ax turn exceeded its 360s budget and was terminated (OPENNEKO_AGENT_TURN_TIMEOUT_MS overrides)",
    );
    expect(message).toContain("time limit");
    expect(message).not.toContain("agent became unavailable");
  });

  it("treats an unavailable openneko serve as the runtime vanishing", () => {
    expect(
      profileBuildFailureMessage(
        "openneko serve is unavailable: connect ENOENT /run/openneko/serve.sock",
      ),
    ).toContain("agent became unavailable");
  });

  it("keeps serving-status recovery for an actual sandbox contract failure", () => {
    expect(
      profileBuildFailureMessage(
        "agent sandbox exited 1: builtin-skills not found",
      ),
    ).toContain("agent became unavailable");
  });

  it("classifies a missing model configuration as a worker handoff failure", () => {
    const message = profileBuildFailureMessage(
      "Ax backend has no model configured. Set the primary provider in admin settings.",
    );
    expect(message).toContain("worker could not load the saved model configuration");
    expect(message).not.toContain("model provider rejected");
  });

  it("does not expose an unknown persisted error to the browser", () => {
    const internal = "unexpected /private/path containing sensitive context";
    expect(profileBuildFailureMessage(internal)).not.toContain(internal);
  });
});
