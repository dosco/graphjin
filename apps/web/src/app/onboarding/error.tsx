"use client";

import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import OnboardingUnavailable from "./OnboardingUnavailable";

export default function OnboardingError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    console.error("[onboarding] failed to render", error);
  }, [error]);

  return (
    <OnboardingUnavailable
      retryAction={
        <Button
          variant="primary"
          size="lg"
          onClick={reset}
        >
          Retry
        </Button>
      }
    />
  );
}
