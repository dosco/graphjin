type PageHeadingProps = {
  eyebrow?: React.ReactNode;
  title: string;
  description?: string;
  meta?: React.ReactNode;
  actions?: React.ReactNode;
};

export default function PageHeading({
  eyebrow,
  title,
  description,
  meta,
  actions,
}: PageHeadingProps) {
  return (
    <header className="page-heading">
      <div className="page-heading-copy">
        {eyebrow || meta ? (
          <div className="page-heading-eyebrow">
            {eyebrow ? <span className="page-heading-mark" aria-hidden="true" /> : null}
            {eyebrow}
            {meta ? <span className="page-heading-meta">{meta}</span> : null}
          </div>
        ) : null}
        <h1>{title}</h1>
        {description ? <p>{description}</p> : null}
      </div>
      {actions ? <div className="page-heading-actions">{actions}</div> : null}
    </header>
  );
}
