import * as React from "react";

import { Input } from "./input.js";

export function normalizeDecimalInput(raw: string): string {
  let value = raw.replace(/[^0-9.,-]/g, "");

  const isNegative = value.startsWith("-");
  value = value.replace(/-/g, "");

  let seenSeparator = false;
  value = value.replace(/[.,]/g, () => {
    if (seenSeparator) return "";
    seenSeparator = true;
    return ".";
  });

  return (isNegative ? "-" : "") + value;
}

export type DecimalInputProps = Omit<React.ComponentProps<"input">, "type">;

function DecimalInput({ inputMode, onChange, ...props }: DecimalInputProps) {
  const handleChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    event.target.value = normalizeDecimalInput(event.target.value);
    onChange?.(event);
  };

  return (
    <Input
      type="text"
      inputMode={inputMode ?? "decimal"}
      onChange={handleChange}
      {...props}
    />
  );
}

export { DecimalInput };
