import { test, expect } from "@playwright/test";
import { dependencyLayout, dependencyRelations } from "../src/v2/dependency-layout";

test("roots and isolated jobs share the top row; children follow longest dependency depth", () => {
  const p = dependencyLayout(["a", "b", "c", "isolated", "root"], [
    { source: "a", target: "b" }, { source: "b", target: "c" }, { source: "root", target: "c" },
  ], 220, 72);
  for (const id of ["a", "isolated", "root"]) expect(p.get(id)!.y).toBe(0);
  expect(p.get("b")!.y).toBe(116);
  expect(p.get("c")!.y).toBe(232);
  const roots = ["a", "isolated", "root"].map(id => p.get(id)!.x).sort((a, b) => a - b);
  expect(roots[1] - roots[0]).toBe(244);
  expect(roots[2] - roots[1]).toBe(244);
});

test("cycles terminate and cannot push disconnected roots below the flow", () => {
  const edges = [{ source: "a", target: "b" }, { source: "b", target: "a" }, { source: "b", target: "c" }];
  const p = dependencyLayout(["a", "b", "c", "isolated"], edges, 220, 72);
  expect(p.get("a")!.y).toBe(p.get("b")!.y);
  expect(p.get("c")!.y).toBeGreaterThan(p.get("b")!.y);
  expect(p.get("isolated")!.y).toBe(0);
  expect(dependencyLayout(["isolated", "c", "b", "a"], edges, 220, 72)).toEqual(p);
});

test("late-named roots do not reserve empty columns for their descendants", () => {
  const p = dependencyLayout(["a-child", "b-child", "z-root"], [{ source: "z-root", target: "a-child" }, { source: "a-child", target: "b-child" }], 220, 72);
  for (const position of p.values()) expect(position.x).toBe(0);
});
test("highlight traverses ancestors and descendants without siblings or cycle loops", () => {
  const edges = [{source:"a",target:"b"},{source:"b",target:"c"},{source:"c",target:"d"},{source:"a",target:"sibling"},{source:"d",target:"c"}];
  const r = dependencyRelations(new Set(["b"]), edges);
  expect([...r.ancestors]).toEqual(["a"]);
  expect([...r.descendants]).toEqual(["c", "d"]);
  expect(dependencyRelations(new Set(), edges).ancestors.size).toBe(0);
});
