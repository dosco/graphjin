"use client";

import { useDensity } from "@/components/DensityProvider";
import { Segment, SegmentedControl } from "@/components/ui/tabs";

// Comfortable / Compact segmented control. Lives in the app header; flips the
// `data-density` attribute that the whole UI keys off.
export default function DensityToggle() {
  const { density, setDensity } = useDensity();
  return (
    <SegmentedControl
      className="density-seg"
      role="group"
      aria-label="Information density"
    >
      <Segment
        selected={density === "comfortable"}
        onClick={() => setDensity("comfortable")}
        title="Roomy — one column"
      >
        <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
          <rect x="2" y="3" width="12" height="3.4" rx="1.2" />
          <rect x="2" y="9.6" width="12" height="3.4" rx="1.2" />
        </svg>
        <span>Comfortable</span>
      </Segment>
      <Segment
        selected={density === "compact"}
        onClick={() => setDensity("compact")}
        title="Dense — tiled grid"
      >
        <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true">
          <rect x="2" y="2.5" width="5" height="5" rx="1.2" />
          <rect x="9" y="2.5" width="5" height="5" rx="1.2" />
          <rect x="2" y="8.5" width="5" height="5" rx="1.2" />
          <rect x="9" y="8.5" width="5" height="5" rx="1.2" />
        </svg>
        <span>Compact</span>
      </Segment>
    </SegmentedControl>
  );
}
