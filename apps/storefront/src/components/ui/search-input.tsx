import type { ComponentPropsWithRef } from "react";

import { SearchIcon } from "./icons";
import { TextInput } from "./text-input";

type SearchInputProps = Omit<
  ComponentPropsWithRef<typeof TextInput>,
  "icon" | "label" | "type"
> & {
  label?: string;
};

export function SearchInput({ label = "Поиск по магазину", ...props }: SearchInputProps) {
  return (
    <TextInput
      icon={<SearchIcon />}
      label={label}
      type="search"
      visuallyHideLabel
      {...props}
    />
  );
}
