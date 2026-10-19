import { test } from "node:test";
import assert from "node:assert/strict";

// Pure unit / interaction tests for Custom Select logic & accessibility
import {
  calculateNextIndex,
  getSelectAriaAttributes,
  filterOptions,
  formatOptionLabel,
} from "../src/select-logic.ts";

test("calculateNextIndex handles ArrowDown, ArrowUp, Home, End with cyclic navigation and disabled items", () => {
  const options = [
    { value: "a", label: "Alpha" },
    { value: "b", label: "Beta", disabled: true },
    { value: "c", label: "Gamma" },
    { value: "d", label: "Delta" },
  ];

  // From index 0 ArrowDown should skip disabled 'b' (index 1) to 'c' (index 2)
  assert.equal(calculateNextIndex(0, "ArrowDown", options), 2);

  // From index 2 ArrowDown to 'd' (index 3)
  assert.equal(calculateNextIndex(2, "ArrowDown", options), 3);

  // From index 3 ArrowDown wraps around to 0
  assert.equal(calculateNextIndex(3, "ArrowDown", options), 0);

  // From index 0 ArrowUp wraps to 3
  assert.equal(calculateNextIndex(0, "ArrowUp", options), 3);

  // From index 2 ArrowUp skips disabled 1 to 0
  assert.equal(calculateNextIndex(2, "ArrowUp", options), 0);

  // Home key goes to first non-disabled option (0)
  assert.equal(calculateNextIndex(2, "Home", options), 0);

  // End key goes to last non-disabled option (3)
  assert.equal(calculateNextIndex(0, "End", options), 3);
});

test("calculateNextIndex returns -1 when all options are disabled or list is empty", () => {
  assert.equal(calculateNextIndex(0, "ArrowDown", []), -1);
  assert.equal(
    calculateNextIndex(0, "ArrowDown", [{ value: "1", label: "One", disabled: true }]),
    -1
  );
});

test("getSelectAriaAttributes returns proper ARIA roles and states", () => {
  const closed = getSelectAriaAttributes({
    expanded: false,
    listboxId: "test-listbox",
    disabled: false,
    required: true,
  });
  assert.equal(closed.role, "combobox");
  assert.equal(closed["aria-expanded"], false);
  assert.equal(closed["aria-haspopup"], "listbox");
  assert.equal(closed["aria-controls"], undefined);
  assert.equal(closed["aria-required"], true);

  const open = getSelectAriaAttributes({
    expanded: true,
    listboxId: "test-listbox",
    disabled: false,
    required: false,
  });
  assert.equal(open["aria-expanded"], true);
  assert.equal(open["aria-controls"], "test-listbox");
});

test("filterOptions performs case-insensitive substring search", () => {
  const options = [
    { value: "gpt-4", label: "GPT-4 Turbo" },
    { value: "claude-3", label: "Claude 3.5 Sonnet" },
    { value: "gemini-pro", label: "Gemini 1.5 Pro" },
  ];
  assert.equal(filterOptions(options, "pro").length, 1);
  assert.equal(filterOptions(options, "pro")[0].value, "gemini-pro");
  assert.equal(filterOptions(options, "turbo").length, 1);
  assert.equal(filterOptions(options, "").length, 3);
});

test("formatOptionLabel provides label with optional fallback or empty handling", () => {
  assert.equal(formatOptionLabel({ value: "v1", label: "Option 1" }), "Option 1");
  assert.equal(formatOptionLabel({ value: "v2" }), "v2");
  assert.equal(formatOptionLabel({ value: "", label: "请选择" }), "请选择");
});
