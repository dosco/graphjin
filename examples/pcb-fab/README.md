# PCB Fab Agentic Demo

The PCB fab demo is a compact electronics-manufacturing stack for exercising
GraphJin's agentic surfaces end to end:

- `mes`: writable Postgres MES records for customers, designs, orders, panels,
  BOM rows, shipments, and NCRs.
- `yield_warehouse`: Snowflake emulator analytics for yield, defects, process
  capability, and WIP aging.
- `test_measurements`: MongoDB station/test documents with explicit
  relationships back into MES context.
- `design_files`: local Gerber/drill/stackup files.
- `supplier_api`: OpenAPI-backed supplier part lookups joined onto BOM items.
- `dfm_code`: TypeScript CodeSQL context for DFM, impedance, and panelization
  rules.
- `workflows`: JavaScript checks for DFM gates, yield triage, and order
  release.

Run the demo:

```bash
GO_ENV=agentic go run ./cmd serve --demo --path examples/pcb-fab
```

Run the smoke suite:

```bash
examples/pcb-fab/scripts/smoke.sh --no-agent
```

The smoke script starts the local supplier mock API on port `8093` before it
queries the OpenAPI join.
