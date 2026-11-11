import { test, expect } from "@playwright/test";
import { mapKeyAction, clampMapSize, resizeMapKey } from "../src/v2/minimap-controls";

test("map arrows and Shift arrows have separate navigation targets", () => {
  for (const [key, dx, dy] of [["ArrowLeft", -30, 0], ["ArrowRight", 30, 0], ["ArrowUp", 0, -30], ["ArrowDown", 0, 30]] as const) {
    expect(mapKeyAction({ key })).toEqual({ kind: "panMap", dx, dy });
    expect(mapKeyAction({ key, shiftKey: true })).toEqual({ kind: "panCanvas", dx, dy });
  }
});

test("map shortcuts preserve browser modifiers and Tab navigation", () => {
  for (const key of ["+", "-", "Home", "ArrowLeft", "Enter"]) {
    for (const modifier of ["ctrlKey", "metaKey", "altKey"]) expect(mapKeyAction({ key, [modifier]: true })).toBeNull();
  }
  expect(mapKeyAction({ key: "Tab" })).toBeNull();
  expect(mapKeyAction({ key: "Tab", shiftKey: true })).toBeNull();
  expect(mapKeyAction({ key: "+", shiftKey: true })).toEqual({ kind: "zoom", factor: 1.5 });
  expect(mapKeyAction({ key: "=" })).toEqual({ kind: "zoom", factor: 1.5 });
  expect(mapKeyAction({ key: "-" })).toEqual({ kind: "zoom", factor: 1 / 1.5 });
  expect(mapKeyAction({ key: "Home" })).toEqual({ kind: "fit" });
  expect(mapKeyAction({ key: "Enter" })).toEqual({ kind: "focus" });
  expect(mapKeyAction({ key: "Escape" })).toEqual({ kind: "dismiss" });
});

test("keyboard resize changes each axis and preserves size bounds", () => {
  const size = { w: 300, h: 200 };
  expect(resizeMapKey(size, { key: "ArrowRight" })).toEqual({ w: 310, h: 200 });
  expect(resizeMapKey(size, { key: "ArrowLeft" })).toEqual({ w: 290, h: 200 });
  expect(resizeMapKey(size, { key: "ArrowDown", shiftKey: true })).toEqual({ w: 300, h: 240 });
  expect(resizeMapKey(size, { key: "ArrowUp", shiftKey: true })).toEqual({ w: 300, h: 160 });
  expect(resizeMapKey({ w: 560, h: 380 }, { key: "ArrowRight" })).toEqual({ w: 560, h: 380 });
  expect(resizeMapKey({ w: 260, h: 110 }, { key: "ArrowUp" })).toEqual({ w: 260, h: 110 });
  expect(resizeMapKey(size, { key: "Home" })).toEqual({ w: 260, h: 168 });
  expect(clampMapSize({ w: -1000, h: 1000 })).toEqual({ w: 260, h: 380 });
  expect(size).toEqual({ w: 300, h: 200 });
});

test("resize leaves browser shortcuts and focus traversal untouched", () => {
  const size = { w: 300, h: 200 };
  for (const key of ["Home", "ArrowLeft", "ArrowDown"]) {
    for (const modifier of ["ctrlKey", "metaKey", "altKey"]) expect(resizeMapKey(size, { key, [modifier]: true })).toBeNull();
  }
  for (const key of ["Tab", "Enter", "Escape"]) expect(resizeMapKey(size, { key })).toBeNull();
});
