import { test, expect } from "@playwright/test";
import { dependencyLayout, dependencyRelations } from "../src/v2/dependency-layout";

test("roots and isolated jobs share the top row; children follow longest dependency depth", () => {
  const p = dependencyLayout(["a", "b", "c", "isolated", "root"], [
    { source: "a", target: "b" }, { source: "b", target: "c" }, { source: "root", target: "c" },
  ], 220, 72);
  for (const id of ["a", "isolated", "root"]) expect(p.get(id)!.y).toBe(0);
  expect(p.get("b")!.y).toBe(152);
  expect(p.get("c")!.y).toBe(304);
  const roots = ["a", "isolated", "root"].map(id => p.get(id)!.x).sort((a, b) => a - b);
  expect(roots[1] - roots[0]).toBeGreaterThanOrEqual(320);
  expect(roots[2] - roots[1]).toBeGreaterThanOrEqual(320);
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


test("layer ordering removes crossed branches and centers forks and joins", () => {
  const edges = [{source:"a",target:"d"},{source:"b",target:"c"},{source:"d",target:"join"},{source:"c",target:"join"}];
  const p = dependencyLayout(["a","b","c","d","join"], edges, 220, 96);
  expect((p.get("a")!.x-p.get("b")!.x)*(p.get("d")!.x-p.get("c")!.x)).toBeGreaterThan(0);
  expect(p.get("join")!.x).toBe((p.get("c")!.x+p.get("d")!.x)/2);
  expect(dependencyLayout(["join","d","c","b","a"], [...edges].reverse(), 220, 96)).toEqual(p);
});

test("empty graphs, unknown edges, self-loops and dense DAGs remain finite and separated", () => {
  expect(dependencyLayout([], [], 220, 96).size).toBe(0);
  const ids=Array.from({length:40},(_,i)=>String(i).padStart(2,"0"));
  const edges=ids.flatMap((source,i)=>ids.slice(i+1).filter((_,j)=>j%3===0).map(target=>({source,target})));
  edges.push({source:"00",target:"missing"},{source:"00",target:"00"});
  const p=dependencyLayout(ids,edges,220,96);
  expect(p.size).toBe(ids.length);
  for (const [i,a] of [...p.values()].entries()) {
    expect(Number.isFinite(a.x)&&Number.isFinite(a.y)).toBe(true);
    for(const b of [...p.values()].slice(i+1)) expect(Math.abs(a.x-b.x)>=320 || Math.abs(a.y-b.y)>=176).toBe(true);
  }
});
