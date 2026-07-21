// Test state: 100 RPS, 1 min
// Mix: 60% POST single | 10% POST batch | 20% GET range | 10% GET anomaly
//
// NAO falha por thresholds: o cenario apenas reporta os trends de latencia por
// operacao (e o http_req_failed) no summary. A avaliacao e leitura humana.
//
// Rodar: k6 run --env BASE_URL=http://<url/ip>:8080 test.js

import http from 'k6/http';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';
import {
  BASE, deviceId, telemetryPayload, telemetryBatchPayload,
  tagged, pickOp, MIX_STEADY, seedDevices,
} from './lib/helpers.js';

export const options = {
  discardResponseBodies: true,
  scenarios: {
    steady: {
      executor: 'constant-arrival-rate',
      rate: 100,
      timeUnit: '1s',
      duration: '1m',
      preAllocatedVUs: 100,
      maxVUs: 400,
    },
  },
  // Sem gate de pass/fail. Estas expressoes sao SEMPRE verdadeiras; existem
  // apenas para o k6 criar as sub-metricas por tag (op:*), que de outro modo
  // nao apareceriam no summary. A "validacao" passa a ser leitura humana dos
  // trends impressos por handleSummary.
  thresholds: {
    http_req_failed:                 ['rate>=0'],
    'http_req_duration{op:post}':    ['p(99)>=0'],
    'http_req_duration{op:batch}':   ['p(99)>=0'],
    'http_req_duration{op:range}':   ['p(99)>=0'],
    'http_req_duration{op:anomaly}': ['p(99)>=0'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'p(95)', 'p(99)', 'p(99.9)', 'max'],
};

// Pre-seed dos devices: garante >= 8 amostras por device antes do load, para
// que as chamadas de anomaly nao retornem 404 "not enough samples" no cold
// start (que contariam como http_req_failed e poluiriam a metrica no summary).
export function setup() {
  seedDevices();
}

export default function () {
  const id = deviceId();
  const op = pickOp(Math.random(), MIX_STEADY);

  switch (op) {
    case 'post':
      http.post(`${BASE}/devices/${id}/telemetry`, telemetryPayload(), tagged('post'));
      break;
    case 'batch':
      http.post(`${BASE}/devices/${id}/telemetry/batch`, telemetryBatchPayload(), tagged('batch'));
      break;
    case 'range': {
      const now = Date.now();
      http.get(`${BASE}/devices/${id}/telemetry?from=${now - 60000}&to=${now}&limit=100`, tagged('range'));
      break;
    }
    case 'anomaly':
      http.get(`${BASE}/devices/${id}/anomaly`, tagged('anomaly'));
      break;
  }
}

// Metricas exibidas no summary. Unico ponto para incluir/excluir (ex.: para
// somar http_reqs ou iterations, basta adicionar aqui).
const SUMMARY_METRICS = [
  'http_req_duration{op:post}',
  'http_req_duration{op:batch}',
  'http_req_duration{op:range}',
  'http_req_duration{op:anomaly}',
  'http_req_failed',
];

export function handleSummary(data) {
  // stdout enxuto: so as metricas de interesse, sem as linhas de threshold.
  // IMPORTANTE: nao reatribuir data.metrics. O bench runner consome o
  // --summary-export (JSON do proprio `data`) e precisa da metrica-base
  // http_req_duration presente para renderizar as sub-metricas por op como
  // filhas dela; se removermos a base, o runner pula o grupo inteiro e so
  // sobra http_req_failed. Entao filtramos numa `view` a parte para o stdout e
  // apenas deletamos os thresholds das metricas (some o gate verde tanto no
  // stdout quanto na secao Thresholds do summary JSON).
  const view = { ...data, metrics: {} };
  for (const name of SUMMARY_METRICS) {
    const m = data.metrics[name];
    if (!m) continue;
    delete m.thresholds; // some com as linhas "✓ p(99)>=0"
    view.metrics[name] = m;
  }
  return {
    stdout: '\n' + textSummary(view, { indent: '  ', enableColors: true }),
  };
}
