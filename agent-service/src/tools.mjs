// The 5 spec-chat customTools (pi-sdk PATTERN §3 [KU8]: defineTool,
// not extensions). Each is a thin wrapper over the CuratorClient —
// zero logic here; the engine lives behind the Go curator-api. The
// gate (src/gate.mjs) enforces HITL/spend; these just call + return.
import { defineTool } from "@earendil-works/pi-coding-agent";
import { Type } from "@earendil-works/pi-ai";

const ok = (obj) => ({ content: [{ type: "text", text: JSON.stringify(obj) }], details: {} });
const err = (e) => ({
  content: [{ type: "text", text: JSON.stringify({ error: String(e?.message || e) }) }],
  details: {},
  isError: true,
});

const editsSchema = Type.Array(
  Type.Object({
    op: Type.String(), // set_page_intent|set_section_intent|set_section_sources|add_section_source
    ref: Type.String(), // "parent" | child page title
    section: Type.Optional(Type.String()),
    value: Type.Optional(Type.String()),
    source: Type.Optional(Type.String()),
    sources: Type.Optional(Type.Array(Type.String())),
  })
);

export function makeTools(client) {
  const read_kb_area = defineTool({
    name: "read_kb_area",
    label: "read kb area",
    description: "Read a mykb knowledge area's entries (facts/decisions/gotchas/patterns).",
    parameters: Type.Object({ area: Type.String() }),
    execute: async (_id, { area }) => {
      try { return ok(await client.readKbArea(area)); } catch (e) { return err(e); }
    },
  });

  const get_doc_spec = defineTool({
    name: "get_doc_spec",
    label: "get doc-spec",
    description: "Read a .doc.yaml cluster spec: its raw yaml plus parsed pages/sections/sources.",
    parameters: Type.Object({ id: Type.String() }),
    execute: async (_id, { id }) => {
      try { return ok(await client.getDocSpec(id)); } catch (e) { return err(e); }
    },
  });

  const preview_spec = defineTool({
    name: "preview_spec",
    label: "preview spec",
    description:
      "Render a candidate spec and run the hardened Judge on it (composite). " +
      "Pass edits to preview an UNSAVED change; nothing is written. Bounded by the spend gate.",
    parameters: Type.Object({ id: Type.String(), edits: Type.Optional(editsSchema) }),
    execute: async (_id, { id, edits }) => {
      try { return ok(await client.previewSpec(id, edits)); } catch (e) { return err(e); }
    },
  });

  const put_doc_spec = defineTool({
    name: "put_doc_spec",
    label: "put doc-spec",
    description:
      "Apply edits to a .doc.yaml IN PLACE (incl. widen-sources). MUTATION — " +
      "the human must approve the diff first (the gate enforces this).",
    parameters: Type.Object({ id: Type.String(), edits: editsSchema }),
    execute: async (_id, { id, edits }) => {
      try { return ok(await client.putDocSpec(id, edits)); } catch (e) { return err(e); }
    },
  });

  const propose_kb_entry = defineTool({
    name: "propose_kb_entry",
    label: "propose kb entry",
    description:
      "Add a knowledge entry to a kb area to close a brain-content gap. MUTATION — " +
      "source (provenance) is mandatory; lands unverified; human must approve first.",
    parameters: Type.Object({
      area: Type.String(),
      type: Type.String(), // fact|decision|gotcha|pattern
      text: Type.String(),
      source: Type.String(),
      why: Type.Optional(Type.String()),
    }),
    execute: async (_id, a) => {
      try { return ok(await client.proposeKbEntry(a)); } catch (e) { return err(e); }
    },
  });

  return [read_kb_area, get_doc_spec, preview_spec, put_doc_spec, propose_kb_entry];
}
