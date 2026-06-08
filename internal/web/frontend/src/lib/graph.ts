import type { GraphData } from "./api-types";

// force-graph wants links that reference node ids; our API sends link endpoints
// as indices into the nodes array (compact wire format). This pure mapping is
// unit-tested (the renderer itself needs a canvas, so this is the verifiable
// seam between the API and force-graph).

export interface FGNode {
  id: string;
  title: string;
  url: string;
  folder: string;
  deg: number;
}

export interface FGLink {
  source: string;
  target: string;
}

export function toForceGraph(data: GraphData): { nodes: FGNode[]; links: FGLink[] } {
  const nodes: FGNode[] = data.nodes.map((n) => ({
    id: n.id,
    title: n.title,
    url: n.url,
    folder: n.folder,
    deg: n.deg,
  }));
  const links: FGLink[] = [];
  for (const l of data.links) {
    const s = data.nodes[l.s];
    const t = data.nodes[l.t];
    if (s && t) links.push({ source: s.id, target: t.id });
  }
  return { nodes, links };
}
