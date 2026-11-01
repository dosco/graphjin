"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { PRINT_PREPARE_EVENT, type PrintPrepareDetail } from "@/lib/print-thread";
import { registerComponent } from "./renderer";
import type { RenderContext } from "./renderer";
import type { A2UIComponent } from "./types";
import type { AnswerMapProps, MapPoint } from "./catalog";
import { AskCard } from "./ask";
import { drillPrompt } from "./drill";

type Rect = { x: number; y: number; w: number; h: number };

function isCurrency(valueLabel: string): boolean {
  return /usd|\$/i.test(valueLabel);
}

export function formatMapValue(value: number, valueLabel: string, compact: boolean): string {
  const currency = isCurrency(valueLabel);
  return new Intl.NumberFormat("en-US", {
    ...(currency ? { style: "currency", currency: "USD" } : {}),
    ...(compact
      ? { notation: "compact", maximumSignificantDigits: 3 }
      : { maximumFractionDigits: currency ? 0 : 2 }),
  }).format(value);
}

export function validMapPoints(points: unknown): MapPoint[] {
  if (!Array.isArray(points)) return [];
  return points.filter((point): point is MapPoint =>
    Boolean(point) && typeof point.label === "string" && point.label.trim() !== "" &&
    Number.isFinite(point.lat) && Math.abs(point.lat) <= 90 &&
    Number.isFinite(point.lon) && Math.abs(point.lon) <= 180 &&
    Number.isFinite(point.v) && point.v >= 0,
  );
}

function overlaps(a: Rect, b: Rect): boolean {
  return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}

const DEFAULT_TILE_STYLE = "https://tiles.openfreemap.org/styles/positron";

/** The installation's tile style: "off", a self-hosted style URL, or OpenFreeMap. */
function tileStyle(): string {
  return document.documentElement.dataset.mapStyle || DEFAULT_TILE_STYLE;
}
const TILE_TIMEOUT_MS = 8_000;
const MAX_PINS = 5;

type Shared = {
  props: AnswerMapProps & { id: string };
  ctx: RenderContext;
  points: MapPoint[];
  active: string | null;
  setActive: React.Dispatch<React.SetStateAction<string | null>>;
};

function token(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function reducedMotion(): boolean {
  return window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
}

type Pin = { label: string; v: number; x: number; y: number };

/** Keep the largest places' labels that fit without overlapping. */
export function placePins(candidates: Pin[], width: number, height: number): Pin[] {
  const taken: Rect[] = [];
  const pins: Pin[] = [];
  for (const pin of [...candidates].sort((a, b) => b.v - a.v)) {
    if (pins.length >= MAX_PINS) break;
    const w = Math.max(pin.label.length, 6) * 7 + 64;
    const rect = { x: pin.x - w / 2, y: pin.y - 46, w, h: 28 };
    if (rect.x < 4 || rect.x + rect.w > width - 4 || rect.y < 4 || pin.y > height - 4) continue;
    if (taken.some((other) => overlaps(rect, other))) continue;
    taken.push(rect);
    pins.push(pin);
  }
  return pins;
}

function TileMap({ props, ctx, points, active, setActive, onFail }: Shared & { onFail: () => void }) {
  const container = useRef<HTMLDivElement | null>(null);
  const mapRef = useRef<import("maplibre-gl").Map | null>(null);
  const activeRef = useRef<string | null>(null);
  const [ready, setReady] = useState(false);
  const [pins, setPins] = useState<Pin[]>([]);
  const [tip, setTip] = useState<Pin | null>(null);
  const indexOf = useRef(new Map<string, number>());
  const snapshot = useRef<HTMLImageElement | null>(null);
  const pointsKey = JSON.stringify(points);

  useEffect(() => {
    // WebGL does not print reliably, so a print shows an image of the map as drawn.
    const capture = () => {
      const map = mapRef.current;
      if (!map || !snapshot.current) return;
      try {
        snapshot.current.src = map.getCanvas().toDataURL("image/png");
      } catch {
        snapshot.current.removeAttribute("src");
      }
    };
    const prepare = (event: Event) => {
      const map = mapRef.current;
      if (!map) return;
      (event as CustomEvent<PrintPrepareDetail>).detail?.wait(new Promise<void>((resolve) => {
        const done = () => { capture(); resolve(); };
        const fallback = window.setTimeout(done, 3_000);
        map.once("idle", () => { window.clearTimeout(fallback); done(); });
        map.resize();
        map.triggerRepaint();
      }));
    };
    window.addEventListener(PRINT_PREPARE_EVENT, prepare);
    window.addEventListener("beforeprint", capture);
    return () => {
      window.removeEventListener(PRINT_PREPARE_EVENT, prepare);
      window.removeEventListener("beforeprint", capture);
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    let map: import("maplibre-gl").Map | null = null;
    let loaded = false;
    let timer = 0;
    const data = JSON.parse(pointsKey) as MapPoint[];
    const peak = Math.max(...data.map((point) => point.v), 1);
    indexOf.current = new Map(data.map((point, index) => [point.label, index]));

    (async () => {
      const [{ Map: MapLibre, NavigationControl, AttributionControl, LngLatBounds, setWorkerUrl }, , { mapWorkerUrl }] = await Promise.all([
        import("maplibre-gl"),
        import("maplibre-gl/dist/maplibre-gl.css"),
        import("./answer-map-worker"),
      ]);
      setWorkerUrl(await mapWorkerUrl());
      if (cancelled || !container.current) return;
      const accent = token("--accent");
      const ink = token("--text");
      const card = token("--card");
      try {
        map = new MapLibre({
          container: container.current,
          style: tileStyle(),
          attributionControl: false,
          cooperativeGestures: true,
          dragRotate: false,
          pitchWithRotate: false,
          touchPitch: false,
          maxPitch: 0,
          renderWorldCopies: false,
          canvasContextAttributes: { preserveDrawingBuffer: true },
          fadeDuration: reducedMotion() ? 0 : 150,
        });
      } catch {
        onFail();
        return;
      }
      mapRef.current = map;
      timer = window.setTimeout(() => {
        if (!cancelled && !loaded) onFail();
      }, TILE_TIMEOUT_MS);
      map.touchZoomRotate.disableRotation();
      map.keyboard.disableRotation();
      map.addControl(new NavigationControl({ showCompass: false }), "top-right");
      map.addControl(new AttributionControl({ compact: true }), "bottom-right");

      const bounds = new LngLatBounds();
      for (const point of data) bounds.extend([point.lon, point.lat]);
      const wide = (container.current.clientWidth ?? 600) < 560;
      const single = data.length === 1 || bounds.getNorthEast().distanceTo(bounds.getSouthWest()) < 50;
      map.fitBounds(bounds, {
        padding: wide ? { top: 56, bottom: 28, left: 28, right: 28 } : { top: 64, bottom: 36, left: 56, right: 56 },
        maxZoom: single ? 11 : 14,
        duration: 0,
      });

      map.on("error", () => {
        if (!loaded) onFail();
      });

      const refreshPins = () => {
        if (!map || cancelled) return;
        const { clientWidth, clientHeight } = map.getContainer();
        setPins(placePins(data.map((point) => {
          const { x, y } = map!.project([point.lon, point.lat]);
          return { label: point.label, v: point.v, x, y };
        }), clientWidth, clientHeight));
        const current = activeRef.current ? data.find((point) => point.label === activeRef.current) : undefined;
        setTip(current ? { label: current.label, v: current.v, ...map.project([current.lon, current.lat]) } : null);
      };

      map.on("load", () => {
        if (!map || cancelled) return;
        loaded = true;
        window.clearTimeout(timer);
        map.getContainer().querySelector(".maplibregl-ctrl-attrib")?.classList.remove("maplibregl-compact-show");
        for (const layer of map.getStyle().layers ?? []) {
          if (layer.type === "symbol") map.setPaintProperty(layer.id, "text-opacity", 0.55);
        }
        map.addSource("answer-points", {
          type: "geojson",
          data: {
            type: "FeatureCollection",
            features: data.map((point, index) => ({
              type: "Feature",
              id: index,
              properties: { label: point.label, r: Math.max(0.16, Math.sqrt(point.v / peak)) },
              geometry: { type: "Point", coordinates: [point.lon, point.lat] },
            })),
          },
        });
        const radius = (factor: number) => [
          "interpolate", ["linear"], ["zoom"],
          0, ["*", ["get", "r"], 14 * factor],
          4, ["*", ["get", "r"], 26 * factor],
          10, ["*", ["get", "r"], 30 * factor],
        ] as unknown as number;
        map.addLayer({
          id: "answer-points-glow",
          type: "circle",
          source: "answer-points",
          paint: {
            "circle-radius": radius(1.5),
            "circle-color": accent,
            "circle-blur": 0.9,
            "circle-opacity": ["case", ["boolean", ["feature-state", "active"], false], 0.32, 0.14] as unknown as number,
          },
        });
        map.addLayer({
          id: "answer-points",
          type: "circle",
          source: "answer-points",
          paint: {
            "circle-radius": radius(1),
            "circle-color": accent,
            "circle-opacity": ["case", ["boolean", ["feature-state", "active"], false], 0.6, 0.34] as unknown as number,
            "circle-stroke-color": ["case", ["boolean", ["feature-state", "active"], false], ink, accent] as unknown as string,
            "circle-stroke-width": ["case", ["boolean", ["feature-state", "active"], false], 2.5, 1.5] as unknown as number,
            "circle-stroke-opacity": 0.95,
          },
        });
        map.addLayer({
          id: "answer-points-core",
          type: "circle",
          source: "answer-points",
          paint: { "circle-radius": 2.5, "circle-color": card, "circle-stroke-color": accent, "circle-stroke-width": 1.5 },
        });
        map.on("mousemove", "answer-points", (event) => {
          const label = event.features?.[0]?.properties?.label;
          if (typeof label === "string") setActive(label);
          map!.getCanvas().style.cursor = drillPrompt(props.drill, String(label)) ? "pointer" : "";
        });
        map.on("mouseleave", "answer-points", () => {
          map!.getCanvas().style.cursor = "";
          setActive(null);
        });
        map.on("click", "answer-points", (event) => {
          const label = event.features?.[0]?.properties?.label;
          if (typeof label !== "string") return;
          setActive(label);
        });
        map.on("move", refreshPins);
        map.on("resize", refreshPins);
        refreshPins();
        setReady(true);
      });
    })().catch(() => {
      if (!cancelled) onFail();
    });

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
      map?.remove();
      mapRef.current = null;
    };
  }, [pointsKey, props.id, props.drill, ctx, onFail, setActive]);

  useEffect(() => {
    const map = mapRef.current;
    const previous = activeRef.current;
    activeRef.current = active;
    if (!map || !ready) return;
    if (previous && indexOf.current.has(previous)) {
      map.setFeatureState({ source: "answer-points", id: indexOf.current.get(previous)! }, { active: false });
    }
    if (active && indexOf.current.has(active)) {
      map.setFeatureState({ source: "answer-points", id: indexOf.current.get(active)! }, { active: true });
      const point = points.find((item) => item.label === active)!;
      setTip({ label: point.label, v: point.v, ...map.project([point.lon, point.lat]) });
    } else {
      setTip(null);
    }
  }, [active, ready, points]);

  return (
    <div className="answer-map-canvas is-tiles" data-ready={ready ? "" : undefined}>
      <div ref={container} className="answer-map-gl" role="img" aria-label={`${props.title}, ${points.length} places on a map`} />
      {/* eslint-disable-next-line @next/next/no-img-element -- holds a data URL captured at print time */}
      <img ref={snapshot} className="answer-map-print" alt="" aria-hidden="true" />
      {ready ? (
        <div className="answer-map-pins" aria-hidden="true">
          {pins.filter((pin) => pin.label !== tip?.label).map((pin) => (
            <span key={pin.label} className="answer-map-pin" style={{ transform: `translate(${pin.x}px, ${pin.y}px)` }}>
              <strong>{pin.label}</strong>
              <span>{formatMapValue(pin.v, props.valueLabel, true)}</span>
            </span>
          ))}
          {tip ? (
            <span className="answer-map-pin is-active" role="status" style={{ transform: `translate(${tip.x}px, ${tip.y}px)` }}>
              <strong>{tip.label}</strong>
              <span>{formatMapValue(tip.v, props.valueLabel, false)}</span>
            </span>
          ) : null}
        </div>
      ) : (
        <div className="answer-map-loading" aria-hidden="true" />
      )}
    </div>
  );
}

function AnswerMap({ props, ctx }: { props: AnswerMapProps & { id: string }; ctx: RenderContext }) {
  const [active, setActive] = useState<string | null>(null);
  const [picked, setPicked] = useState<string | null>(null);
  if (active && active !== picked) setPicked(active);
  const [mode, setMode] = useState<"tiles" | "failed" | "off" | null>(null);
  const points = validMapPoints(props.points);
  const peak = Math.max(...points.map((item) => item.v), 1);
  const fail = useCallback(() => setMode("failed"), []);
  useEffect(() => {
    const id = window.setTimeout(() => setMode(tileStyle() === "off" ? "off" : navigator.onLine === false ? "failed" : "tiles"), 0);
    return () => window.clearTimeout(id);
  }, []);
  const shared: Shared = { props, ctx, points, active, setActive };
  return (
    <figure className="answer-map" aria-label={props.title}>
      <figcaption className="work-chart-title">{props.title}</figcaption>
      {mode === "tiles" ? <TileMap {...shared} onFail={fail} /> : null}
      {mode === "failed" ? <p className="answer-map-unavailable">The map could not load. The places are listed below.</p> : null}
      <ol className="answer-map-rank" aria-label={`${props.title}, ranked by ${props.valueLabel}`}>
        {[...points].sort((a, b) => b.v - a.v).map((point) => {
          const prompt = drillPrompt(props.drill, point.label);
          return (
            <li
              key={point.label}
              className={active === point.label ? "is-active" : undefined}
              onMouseEnter={() => setActive(point.label)}
              onMouseLeave={() => setActive((current) => (current === point.label ? null : current))}
            >
              {prompt ? (
                <Button
                  variant="ghost"
                  className="answer-map-rank-name"
                  title={prompt}
                  onFocus={() => setActive(point.label)}
                  onBlur={() => setActive((current) => (current === point.label ? null : current))}
                  onClick={() => setActive(point.label)}
                >
                  {point.label}
                </Button>
              ) : (
                <span className="answer-map-rank-name">{point.label}</span>
              )}
              <span className="answer-map-rank-value" title={formatMapValue(point.v, props.valueLabel, false)}>
                {formatMapValue(point.v, props.valueLabel, true)}
              </span>
              <span className="answer-map-rank-bar" aria-hidden="true">
                <span style={{ transform: `scaleX(${point.v / peak})` }} />
              </span>
            </li>
          );
        })}
      </ol>
      {picked ? (
        <AskCard
          ctx={ctx}
          componentId={props.id}
          template={props.drill}
          label={picked}
          value={mapValue(points, picked, props.valueLabel)}
        />
      ) : null}
    </figure>
  );
}

function mapValue(points: MapPoint[], label: string, valueLabel: string): string | undefined {
  const point = points.find((item) => item.label === label);
  return point ? formatMapValue(point.v, valueLabel, false) : undefined;
}

registerComponent("AnswerMap", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as AnswerMapProps & { id: string };
  if (typeof props.title !== "string" || typeof props.valueLabel !== "string" || validMapPoints(props.points).length === 0) {
    return null;
  }
  return <AnswerMap key={props.id} props={props} ctx={ctx} />;
});
