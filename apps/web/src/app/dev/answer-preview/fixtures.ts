import type { A2UIMessage } from "@/a2ui/types";

type Component = Record<string, unknown> & { id: string; component: string };

function answer(id: string, title: string, body: Component[], extra: Record<string, unknown> = {}): A2UIMessage[] {
  return [{
    version: "v1.0",
    createSurface: {
      surfaceId: `preview-${id}`,
      catalogId: "urn:openneko:catalog:work:v2",
      dataModel: {},
      components: [
        { id: "root", component: "Answer", title, children: body.map((part) => part.id), ...extra },
        ...body,
      ],
    },
  } as A2UIMessage];
}

const CATEGORY_COLUMNS = [
  { key: "c0", label: "Category" },
  { key: "c1", label: "Units sold", align: "right" },
  { key: "c2", label: "Revenue (USD)", align: "right" },
  { key: "c3", label: "Gross margin (%)", align: "right" },
];
const CATEGORY_ROWS = [
  { c0: "Mountain Bikes", c1: "1,740", c2: "2,323,674", c3: "31.32" },
  { c0: "Touring Bikes", c1: "1,845", c2: "2,089,568", c3: "14.13" },
  { c0: "Road Bikes", c1: "1,822", c2: "1,848,407", c3: "18.79" },
];
const DRILL = "Break down {label} revenue by month for the last 90 days";

export const ANSWER_FIXTURES: Record<string, { label: string; messages: A2UIMessage[] }> = {
  report: {
    label: "Report",
    messages: answer("report", "Bike categories, last 90 days", [
      {
        id: "keyFigures", component: "KeyFigures", items: [
          { label: "Mountain Bikes revenue", value: "$2,323,674.45", sub: "31.32% gross margin · 1,740 units", basis: "calculated", asOf: "08 Sep 2026", source: "sales orders" },
          { label: "Touring Bikes revenue", value: "$2,089,567.66", sub: "14.13% gross margin" },
          { label: "Road Bikes revenue", value: "$1,848,406.70", sub: "18.79% gross margin" },
          { label: "Total bike revenue", value: "$6,261,648.80", sub: "21.88% overall margin" },
        ],
      },
      { id: "chart", component: "Chart", type: "bar", title: "Revenue by category", valueLabel: "Revenue (USD)", drill: DRILL, data: [{ d: "Mountain Bikes", v: 2323674.45 }, { d: "Road Bikes", v: 1848406.7 }, { d: "Touring Bikes", v: 2089567.66 }] },
      {
        id: "table", component: "Table", drill: DRILL,
        columns: [...CATEGORY_COLUMNS, { key: "c4", label: "Gross profit (USD)", align: "right" }, { key: "c5", label: "Return rate (%)", align: "right" }],
        rows: CATEGORY_ROWS.map((row, index) => ({ ...row, c4: ["727,786", "295,166", "347,386"][index]!, c5: ["1.2", "0.8", "1.9"][index]! })),
      },
      { id: "callout", component: "Callout", mood: "watch", text: "Touring Bikes sold the most units but carry the lowest margin, 14.13%." },
      { id: "followUps", component: "Choice", options: [
        { label: "Break down bike sales month over month", prompt: "Break down bike sales month over month" },
        { label: "Compare unit price and unit cost across models", prompt: "Compare unit price and unit cost across models" },
      ] },
    ]),
  },
  compare: {
    label: "Compare",
    messages: answer("compare", "Mountain, Road and Touring side by side", [
      { id: "compare", component: "Compare", columns: CATEGORY_COLUMNS, rows: CATEGORY_ROWS, drill: DRILL },
      { id: "callout", component: "Callout", mood: "good", text: "Mountain Bikes lead on revenue and margin." },
    ]),
  },
  whatif: {
    label: "What-if",
    messages: answer("whatif", "Road bike price change, Q3 2026", [
      {
        id: "whatIf", component: "WhatIf",
        controls: [
          { name: "price_change", label: "Road bike price change", value: 5, min: -20, max: 20, step: 1, unit: "%" },
          { name: "volume_loss", label: "Units lost per 1% price rise", value: 0.6, min: 0, max: 3, step: 0.1, unit: "%" },
        ],
        values: { road_rev: 1496791.67, other_rev: 4532859.65 },
        outputs: [
          { label: "Road bike revenue", unit: "USD", expression: "road_rev * (1 + price_change / 100) * (1 - max(price_change, 0) * volume_loss / 100)" },
          { label: "Total revenue", unit: "USD", expression: "road_rev * (1 + price_change / 100) * (1 - max(price_change, 0) * volume_loss / 100) + other_rev" },
          { label: "Change in total", unit: "%", expression: "(road_rev * (1 + price_change / 100) * (1 - max(price_change, 0) * volume_loss / 100) - road_rev) / (road_rev + other_rev) * 100" },
        ],
      },
      { id: "callout", component: "Callout", mood: "watch", text: "The volume response is an estimate. Move it to test your own assumption." },
    ]),
  },
  map: {
    label: "Map",
    messages: answer("map", "Sales by territory, last 90 days", [
      {
        id: "map", component: "AnswerMap", title: "Sales by territory", valueLabel: "Sales (USD)", drill: "Show the top customers in {label} for the last 90 days",
        points: [
          { label: "Southwest", lat: 34.05, lon: -112.07, v: 1121473 },
          { label: "Northwest", lat: 46.6, lon: -120.5, v: 904211 },
          { label: "Canada", lat: 49.28, lon: -100.0, v: 812004 },
          { label: "Central", lat: 41.6, lon: -93.6, v: 402118 },
          { label: "Northeast", lat: 42.36, lon: -73.0, v: 301552 },
          { label: "Southeast", lat: 33.75, lon: -84.39, v: 288310 },
          { label: "Australia", lat: -33.87, lon: 151.21, v: 701332 },
          { label: "United Kingdom", lat: 51.51, lon: -0.13, v: 512019 },
          { label: "Germany", lat: 51.17, lon: 10.45, v: 498701 },
          { label: "France", lat: 46.6, lon: 2.35, v: 519114 },
        ],
      },
    ]),
  },
  suburbs: {
    label: "Suburbs",
    messages: answer("suburbs", "Store sales by suburb, last 30 days", [
      {
        id: "map", component: "AnswerMap", title: "Store sales by suburb", valueLabel: "Sales (USD)", drill: "Show the best-selling products in {label}",
        points: [
          { label: "Surry Hills", lat: -33.886, lon: 151.211, v: 184200 },
          { label: "Newtown", lat: -33.897, lon: 151.179, v: 142900 },
          { label: "Bondi Junction", lat: -33.891, lon: 151.250, v: 211400 },
          { label: "North Sydney", lat: -33.839, lon: 151.207, v: 167300 },
          { label: "Glebe", lat: -33.879, lon: 151.186, v: 98100 },
          { label: "Randwick", lat: -33.914, lon: 151.241, v: 121700 },
        ],
      },
    ]),
  },
  diagram: {
    label: "Diagram",
    messages: answer("diagram", "How an order moves to shipment", [
      {
        id: "diagram", component: "Diagram", title: "Order to shipment", drill: "Show the last 10 records in the {label} step",
        nodes: [
          { id: "Customer", label: "Customer" },
          { id: "Sales order", label: "Sales order" },
          { id: "Order lines", label: "Order lines" },
          { id: "Credit check", label: "Credit check" },
          { id: "Inventory", label: "Inventory" },
          { id: "Shipment", label: "Shipment" },
          { id: "Invoice", label: "Invoice" },
        ],
        edges: [
          { from: "Customer", to: "Sales order", label: "places" },
          { from: "Sales order", to: "Order lines", label: "contains" },
          { from: "Sales order", to: "Credit check", label: "approved by" },
          { from: "Order lines", to: "Inventory", label: "reserves" },
          { from: "Credit check", to: "Shipment" },
          { from: "Inventory", to: "Shipment", label: "picks" },
          { from: "Shipment", to: "Invoice", label: "bills" },
        ],
      },
    ]),
  },
  tables: {
    label: "Dense diagram",
    messages: answer("tables", "Order-to-shipment tables", [
      {
        id: "diagram", component: "Diagram", title: "Order-to-shipment tables", drill: "Show the latest rows in {label}",
        nodes: ["Customer (sales.customer)", "Payment (sales.creditcard)", "Addresses (person.address)", "Shipping method (purchasing.shipmethod)", "Product catalog (production.product)", "Promotions (sales.specialofferproduct)", "Order header (sales.salesorderheader)", "Order lines (sales.salesorderdetail)", "Shipment fulfilled (status 5)", "Inventory (production.productinventory)", "Inventory movement (production.transactionhistory)", "Carrier tracking (carriertrackingnumber)"].map((label) => ({ id: label, label })),
        edges: [
          ["Customer (sales.customer)", "Order header (sales.salesorderheader)", "places order"],
          ["Payment (sales.creditcard)", "Order header (sales.salesorderheader)", "authorizes payment"],
          ["Addresses (person.address)", "Order header (sales.salesorderheader)", "supplies bill-to and ship-to"],
          ["Shipping method (purchasing.shipmethod)", "Order header (sales.salesorderheader)", "defines carrier and rates"],
          ["Product catalog (production.product)", "Order lines (sales.salesorderdetail)", "specifies SKU and base price"],
          ["Promotions (sales.specialofferproduct)", "Order lines (sales.salesorderdetail)", "applies discounts"],
          ["Order header (sales.salesorderheader)", "Order lines (sales.salesorderdetail)", "contains line items"],
          ["Order header (sales.salesorderheader)", "Shipment fulfilled (status 5)", "records dispatch date"],
          ["Order lines (sales.salesorderdetail)", "Inventory (production.productinventory)", "checks location stock"],
          ["Order lines (sales.salesorderdetail)", "Inventory movement (production.transactionhistory)", "deducts inventory (type S)"],
          ["Order lines (sales.salesorderdetail)", "Carrier tracking (carriertrackingnumber)", "assigns tracking number"],
        ].map(([from, to, label]) => ({ from, to, label })),
      },
    ]),
  },
  tool: {
    label: "Tool",
    messages: answer("tool", "Reorder calculator", [
      {
        id: "tool", component: "AnswerTool", title: "Reorder quantity",
        html: `<label>Product <select id="p"><option value="12.4">HL Road Frame (12.4/day, 96 on hand)</option><option value="3.1">Touring Tire (3.1/day, 210 on hand)</option></select></label>
<label>Target days of cover <input id="d" type="number" value="30" min="1" max="180"></label>
<output id="o"></output>
<script>
const onHand = { "12.4": 96, "3.1": 210 };
function calc() {
  const rate = Number(p.value); const days = Number(d.value);
  const need = Math.max(0, Math.ceil(rate * days - onHand[p.value]));
  o.textContent = need + " units to reorder";
}
p.oninput = calc; d.oninput = calc; calc();
</script>`,
      },
    ]),
  },
  draft: {
    label: "Draft",
    messages: answer("draft", "Bike categories, last 90 days", [
      { id: "keyFigures", component: "KeyFigures", items: [{ label: "Total bike revenue", value: "$6,261,648.80" }] },
    ], { stage: "draft" }),
  },
};
