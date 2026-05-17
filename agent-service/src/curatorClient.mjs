// Thin HTTP client for the Go curator-api (design D1: HTTP/JSON, no
// shell-scrape). No pi-SDK import — pure fetch, so it is unit-testable
// on any Node. The wire shapes are the contract pinned in
// test/contract/curatorapi_contract_test.go.

export class CuratorClient {
  constructor(baseURL = process.env.CURATOR_API_URL || "http://127.0.0.1:4773") {
    this.baseURL = baseURL.replace(/\/+$/, "");
  }

  async #post(path, body) {
    const res = await fetch(this.baseURL + path, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(body ?? {}),
    });
    const text = await res.text();
    let data;
    try {
      data = text ? JSON.parse(text) : {};
    } catch {
      throw new Error(`curator-api ${path}: non-JSON ${res.status}: ${text.slice(0, 200)}`);
    }
    if (!res.ok) {
      // The API's error contract is {error}; surface it verbatim so
      // the agent (and the gate's reasons) stay faithful.
      throw new Error(`curator-api ${path} ${res.status}: ${data.error || text}`);
    }
    return data;
  }

  readKbArea(area) {
    return this.#post("/v1/kb/area", { area });
  }

  getDocSpec(id) {
    return this.#post("/v1/doc-spec/get", { id });
  }

  // edits: [{op, ref, section?, value?|source?|sources?}]
  putDocSpec(id, edits) {
    return this.#post("/v1/doc-spec/put", { id, edits });
  }

  proposeKbEntry({ area, type, text, source, why }) {
    return this.#post("/v1/kb/propose-entry", { area, type, text, source, why });
  }

  previewSpec(id, edits) {
    return this.#post("/v1/preview", edits && edits.length ? { id, edits } : { id });
  }
}
