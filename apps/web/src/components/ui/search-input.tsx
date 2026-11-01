import { Search } from "lucide-react";
import type { ComponentPropsWithoutRef } from "react";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";

type SearchInputProps = Omit<ComponentPropsWithoutRef<"input">, "type"> & {
  label: string;
};

export function SearchInput({ label, className, ...props }: SearchInputProps) {
  return (
    <label className="block min-w-0" data-ui-list-search="">
      <span className="sr-only">{label}</span>
      <InputGroup className="min-h-10 rounded-control border-[1.5px] border-border bg-card">
        <InputGroupAddon className="pl-3.5 text-text3">
          <Search aria-hidden="true" />
        </InputGroupAddon>
        <InputGroupInput
          type="search"
          aria-label={label}
          className={className}
          {...props}
        />
      </InputGroup>
    </label>
  );
}
