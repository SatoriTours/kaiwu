export interface SelectOption<T = any> {
  value: T;
  label?: string;
  disabled?: boolean;
  hint?: string;
}

export function calculateNextIndex<T>(
  currentIndex: number,
  key: "ArrowDown" | "ArrowUp" | "Home" | "End",
  options: SelectOption<T>[]
): number {
  if (!options || options.length === 0) return -1;
  const enabledIndices = options
    .map((opt, i) => (opt.disabled ? -1 : i))
    .filter((i) => i !== -1);
  if (enabledIndices.length === 0) return -1;

  if (key === "Home") return enabledIndices[0];
  if (key === "End") return enabledIndices[enabledIndices.length - 1];

  if (currentIndex < 0 || !enabledIndices.includes(currentIndex)) {
    return key === "ArrowDown" ? enabledIndices[0] : enabledIndices[enabledIndices.length - 1];
  }

  const currentPos = enabledIndices.indexOf(currentIndex);
  if (key === "ArrowDown") {
    const nextPos = (currentPos + 1) % enabledIndices.length;
    return enabledIndices[nextPos];
  } else {
    const prevPos = (currentPos - 1 + enabledIndices.length) % enabledIndices.length;
    return enabledIndices[prevPos];
  }
}

export function getSelectAriaAttributes(params: {
  expanded: boolean;
  listboxId: string;
  disabled?: boolean;
  required?: boolean;
}) {
  return {
    role: "combobox" as const,
    "aria-expanded": params.expanded,
    "aria-haspopup": "listbox" as const,
    "aria-controls": params.expanded ? params.listboxId : undefined,
    "aria-disabled": params.disabled || undefined,
    "aria-required": params.required || undefined,
  };
}

export function filterOptions<T>(
  options: SelectOption<T>[],
  query: string
): SelectOption<T>[] {
  const q = query.trim().toLowerCase();
  if (!q) return options;
  return options.filter((opt) => {
    const text = (opt.label ?? String(opt.value)).toLowerCase();
    return text.includes(q);
  });
}

export function formatOptionLabel<T>(option: SelectOption<T>): string {
  if (option.label !== undefined && option.label !== null) {
    return option.label;
  }
  return String(option.value);
}
