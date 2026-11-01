import Image from "next/image";

const APP_VERSION = process.env.NEXT_PUBLIC_APP_VERSION ?? "0.0.0";

type EntryShellProps = {
  eyebrow?: string;
  title: string;
  description: string;
  children: React.ReactNode;
  steps?: readonly string[];
  currentStep?: number;
  className?: string;
};

export default function EntryShell({
  eyebrow,
  title,
  description,
  children,
  steps,
  currentStep = 0,
  className,
}: EntryShellProps) {
  return (
    <main className={`entry-shell${className ? ` ${className}` : ""}`}>
      <header className="entry-masthead">
        <a
          className="entry-brand"
          href="https://openneko.app"
          target="_blank"
          rel="noopener noreferrer"
          aria-label="OpenNeko — open website in a new tab"
        >
          <Image src="/cat.png" alt="" width={27} height={28} priority />
          <span>OpenNeko</span>
        </a>
        <span className="entry-version font-mono">{APP_VERSION}</span>
      </header>

      <div className="entry-canvas">
        <aside className="entry-orientation">
          <div>
            {eyebrow ? <p className="entry-eyebrow">{eyebrow}</p> : null}
            <h1>{title}</h1>
            <p className="entry-description">{description}</p>
          </div>

          {steps && steps.length > 0 ? (
            <ol className="entry-steps" aria-label="Setup progress">
              {steps.map((step, index) => {
                const state =
                  index < currentStep
                    ? "is-complete"
                    : index === currentStep
                      ? "is-current"
                      : "";
                return (
                  <li
                    key={step}
                    className={state}
                    aria-current={index === currentStep ? "step" : undefined}
                  >
                    <span className="entry-step-number font-mono">
                      {index < currentStep ? "✓" : String(index + 1).padStart(2, "0")}
                    </span>
                    <span>{step}</span>
                  </li>
                );
              })}
            </ol>
          ) : null}

        </aside>

        <section className="entry-workspace">{children}</section>
      </div>
    </main>
  );
}
