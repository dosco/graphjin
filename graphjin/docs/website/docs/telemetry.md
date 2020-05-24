---
id: telemetry
title: Tracing and Metrics
sidebar_label: Telemetry
---

import useBaseUrl from '@docusaurus/useBaseUrl'; // Add to the top of the file below the front matter.

Having observability and telemetry is at the core of any production ready service. Super Graph has built-in support for OpenCensus for tracing requests all the way from HTTP to the database and providing all kinds of metrics.

OpenCensus has a concept called exporters these are external services that can consume this data and make to give you graphs, charts, alerting etc. Super Graph again has built-in support for Zipkin, Prometheus, Google Stackdriver and the AWS X-Ray exporters.

## Telemetry config

The `telemetry` section of the standard config files is where you set values to configure this feature to your needs.

```yaml
telemetry:
  debug: true
  interval: 5s
  metrics:
    exporter: "prometheus"
    endpoint: ""
    namespace: "web api"
    key: "1234xyz"
  tracing:
    exporter: "zipkin"
    endpoint: "http://zipkin:9411/api/v2/spans"
    sample: 0.2
    include_query: false
    include_params: false
```

**debug**: Enabling debug enables an embedded web ui to test and debug tracing and metrics. This UI called `zPages` is provided by OpenCensus and will be made available on the `/telemetry` path. For more information on using `zPages` https://opencensus.io/zpages/. Remeber to disable this in production.

**interval**: This controls the interval setting for OpenCensus metrics collection. This deafaults to `5 seconds` if not set.

**metric.exporters** Setting this enables metrics collection. The supported values for this field are `prometheus` and `stackdriver`. The Prometheus exporter requires `metric.namespace` to be set. The Sackdriver exporter requires the `metric.key` to be set to the Google Cloud Project ID.

**metric.endpoint** Is not currently used by any of the exporters.

**tracing.exporter** Setting this enables request tracing. The supported values for this field are `zipkin`, `aws` and `xray`. Zipkin requires `tracing.endpoint` to be set. AWS and Xray are the same and do not require any addiitonal settings.

**tracing.sample** This controls what percentage of the requests should be traced. By default `0.5` or 50% of the requests are traced, `always` is also a valid value for this field and it means all requests will be traced.

**include_query** Include the Super Graph SQL query to the trace. Be careful with this setting in production it will add the entire SQL query to the trace. This can be veru useful to debug slow requests.

**include_params** Include the Super Graph SQL query parameters to the trace. Be careful with this setting in production it will it can potentially leak sensitive user information into tracing logs.

## Using Zipkin

Zipkin is a really great open source request tracing project. It's easy to add to your current Super Graph app as a way to test tracing in development. Add the following to the Super Graph generated `docker-compose.yml` file. Also add `zipkin` in your current apps `depends_on` list. Once setup the Zipkin UI is available at http://localhost:9411

```yaml
  your_api:
    ...
    depends_on:
      - db
      - zipkin

  zipkin:
    image: openzipkin/zipkin-slim
    container_name: zipkin
    # Environment settings are defined here https://github.com/openzipkin/zipkin/blob/master/zipkin-server/README.md#environment-variables
    environment:
      - STORAGE_TYPE=mem
      # Uncomment to enable self-tracing
      # - SELF_TRACING_ENABLED=true
      # Uncomment to enable debug logging
      # - JAVA_OPTS=-Dorg.slf4j.simpleLogger.log.zipkin2=debug
    ports:
      # Port used for the Zipkin UI and HTTP Api
      - 9411:9411
```

### Zipkin HTTP to DB traces

<img alt="Zipkin Traces" src={useBaseUrl("img/zipkin1.png")} />

### Zipkin trace details

<img alt="Zipkin Traces" src={useBaseUrl('img/zipkin2.png')} />
