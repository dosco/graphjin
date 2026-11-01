import { useId, type ComponentPropsWithoutRef, type ReactNode } from "react";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/cn";

type FieldProps = {
  label: ReactNode;
  hint?: ReactNode;
  error?: ReactNode;
  htmlFor?: string;
  className?: string;
  children: ReactNode;
};

export function Field({
  label,
  hint,
  error,
  htmlFor,
  className,
  children,
}: FieldProps) {
  return (
    <div data-slot="field" className={cn("grid min-w-0 content-start gap-1.5", className)}>
      <label
        data-slot="field-label"
        htmlFor={htmlFor}
        className="font-body text-ui-caption font-bold text-text2"
      >
        {label}
      </label>
      {children}
      {error ? (
        <span
          data-slot="field-error"
          className="text-ui-caption leading-[var(--leading-compact)] text-danger"
        >
          {error}
        </span>
      ) : hint ? (
        <span
          data-slot="field-description"
          className="text-ui-caption leading-[var(--leading-compact)] text-text3"
        >
          {hint}
        </span>
      ) : null}
    </div>
  );
}

type FieldControlProps = Omit<FieldProps, "htmlFor" | "children"> &
  ComponentPropsWithoutRef<"input">;

export function FieldInput({
  label,
  hint,
  error,
  className,
  id,
  ...props
}: FieldControlProps) {
  const generatedId = useId();
  const controlId = id ?? generatedId;
  return (
    <Field
      label={label}
      hint={hint}
      error={error}
      htmlFor={controlId}
      className={className}
    >
      <Input
        id={controlId}
        aria-invalid={error ? true : undefined}
        {...props}
      />
    </Field>
  );
}

export { Input } from "@/components/ui/input";
export { NativeSelect } from "@/components/ui/native-select";
export { Textarea } from "@/components/ui/textarea";
