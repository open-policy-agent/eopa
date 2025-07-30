import { check } from 'k6';
import http from 'k6/http';
import { Trend, Gauge } from 'k6/metrics';
import { SharedArray } from 'k6/data';

const endpoint = `http://${__ENV.HOST}:8181/v1/data/rbac/allow?metrics`;
const endpointMetrics = `http://${__ENV.HOST}:8181/metrics`;

const queries = new SharedArray('queries', function () {
  return JSON.parse(open(`${__ENV.QUERY_FILE}`));
});

const handlerTimer = new Trend('timer_server_handler_ns');
const heapInuseBytes = new Gauge('heap_inuse_bytes');

export default function () {
  const query = queries[Math.floor(Math.random() * queries.length)];
  const input = query.input;

  const r = http.post(endpoint, JSON.stringify({input}), {
    headers: { 'Content-Type': 'application/json' },
  });
  handlerTimer.add(r.json().metrics.timer_server_handler_ns);

  if (Math.random() <= 0.1) {
    const met = http.get(endpointMetrics);
    for (const line of met.body.split(/\n/)) {
      if (line.startsWith("go_memstats_heap_inuse_bytes")) {
        const [key, val] = line.split(" ");
        heapInuseBytes.add(val);
      }
    }
  }

  check(r, {
    'expected result': r.json().result === query.expected
  });
}

export function handleSummary(data) {
  const rps = data.metrics.iterations.values.rate;
  const heapSizeMax = data.metrics.heap_inuse_bytes.values.max/1024/1024/1024
  const output = `
Results:
  requests per second (mean):   ${round(rps)}
  server heap size (max):       ${round(heapSizeMax)}GB
`
  return {
    stdout: output.trim(),
  };
}

function round(num) {
  return +(Math.round(num + "e+2")  + "e-2");
}
