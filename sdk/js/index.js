import { createHmac } from "node:crypto";

export class KingaClient {
  constructor({ baseUrl, tenant, keyId, secret, fetchImpl = fetch, timeoutMs = 2000 }) {
    const url = new URL(baseUrl);
    if (!["http:", "https:"].includes(url.protocol)) throw new TypeError("baseUrl must use HTTP(S)");
    if (!tenant || !keyId || Buffer.byteLength(secret ?? "") < 16) {
      throw new TypeError("tenant, keyId and at least 16 secret bytes are required");
    }
    this.baseUrl = url;
    this.tenant = tenant;
    this.keyId = keyId;
    this.secret = secret;
    this.fetch = fetchImpl;
    this.timeoutMs = timeoutMs;
  }

  assess(event) {
    if (!event?.tenant_event_id) throw new TypeError("event.tenant_event_id is required");
    return this.#post("/v1/assess", event, event.tenant_event_id);
  }

  tokenize(namespace, value, scope = "network") {
    return this.#post("/v1/tokenize", { namespace, value, scope });
  }

  async #post(path, payload, idempotencyKey) {
    const body = JSON.stringify(payload);
    const timestamp = Math.floor(Date.now() / 1000).toString();
    const signature = createHmac("sha256", this.secret).update(`${timestamp}.${body}`).digest("hex");
    const headers = {
      "content-type": "application/json", "x-kinga-key": this.keyId,
      "x-kinga-tenant": this.tenant, "x-kinga-timestamp": timestamp,
      "x-kinga-signature": signature,
    };
    if (idempotencyKey) headers["idempotency-key"] = idempotencyKey;
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    try {
      const response = await this.fetch(new URL(path, this.baseUrl), {
        method: "POST", headers, body, signal: controller.signal,
      });
      const text = await response.text();
      if (!response.ok) throw new Error(`Kinga API returned HTTP ${response.status}: ${text.slice(0, 4096)}`);
      return JSON.parse(text);
    } finally {
      clearTimeout(timer);
    }
  }
}
