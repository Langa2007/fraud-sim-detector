import http from 'k6/http';
import { check, sleep } from 'k6';
import crypto from 'k6/crypto';

export const options = {
  stages: [
    { duration: '15s', target: 20 },  // Ramp-up
    { duration: '30s', target: 50 },  // Steady load
    { duration: '10s', target: 0 },   // Ramp-down
  ],
  thresholds: {
    http_req_duration: ['p(99)<150'], // Phase 1 gate: p99 latency <= 150 ms
    http_req_failed: ['rate<0.01'],    // Error rate < 1%
  },
};

const BASE_URL = __ENV.KINGA_GATEWAY_URL || 'http://localhost:8080';
const API_KEY = __ENV.KINGA_API_KEY || 'demo-key';
const API_SECRET = __ENV.KINGA_API_SECRET || 'demo-secret';
const TENANT = __ENV.KINGA_TENANT || 'demo';

export default function () {
  const eventId = `load_${__VU}_${__ITER}_${Date.now()}`;
  const now = new Date().toISOString();

  const payload = JSON.stringify({
    tenant_event_id: eventId,
    event_type: 'funds_transfer',
    occurred_at: now,
    channel: 'mobile_app',
    subject: {
      msisdn: '+254712345678',
      account_ref: `acc_${__VU}`,
    },
    device: {
      device_id: `dvc_${__VU}`,
      ip: '197.232.1.10',
    },
    transaction: {
      amount: 15000,
      currency: 'KES',
      rail: 'pesalink',
      beneficiary: {
        type: 'bank_account',
        ref: `ben_${__ITER % 10}`,
      },
    },
  });

  const timestamp = Math.floor(Date.now() / 1000).toString();
  const signatureInput = `${timestamp}.${payload}`;
  const signature = crypto.hmac('sha256', API_SECRET, signatureInput, 'hex');

  const headers = {
    'Content-Type': 'application/json',
    'X-Kinga-Key': API_KEY,
    'X-Kinga-Timestamp': timestamp,
    'X-Kinga-Signature': signature,
    'X-Kinga-Tenant': TENANT,
    'Idempotency-Key': eventId,
  };

  const res = http.post(`${BASE_URL}/v1/assess`, payload, { headers });

  check(res, {
    'status is 200': (r) => r.status === 200,
    'mode is shadow': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.mode === 'shadow';
      } catch (e) {
        return false;
      }
    },
    'has evidence_id': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.evidence_id && body.evidence_id.length > 0;
      } catch (e) {
        return false;
      }
    },
  });

  sleep(0.05);
}
