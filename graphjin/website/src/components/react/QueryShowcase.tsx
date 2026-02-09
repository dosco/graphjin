import React, { useState } from 'react';
import { Terminal, Database } from 'lucide-react';

const examples = [
  {
    id: 'query',
    label: 'Query',
    graphql: `query {
  products(limit: 10, order_by: { price: desc }) {
    id
    name
    price
    owner {
      full_name
      email
    }
  }
}`,
    result: `{
  "products": [
    {
      "id": 104,
      "name": "Mechanical Keyboard",
      "price": 149.99,
      "owner": {
        "full_name": "Alice Dev",
        "email": "alice@example.com"
      }
    }
  ]
}`
  },
  {
    id: 'nested',
    label: 'Nested',
    graphql: `query {
  users {
    full_name
    posts {
      title
      comments {
        body
        author { email }
      }
    }
  }
}`,
    result: `{
  "users": [
    {
      "full_name": "Bob Builder",
      "posts": [
        {
          "title": "Getting Started",
          "comments": [
            {
              "body": "Great post!",
              "author": { "email": "fan@example.com" }
            }
          ]
        }
      ]
    }
  ]
}`
  },
  {
    id: 'mutation',
    label: 'Mutation',
    graphql: `mutation {
  products(insert: {
    name: "New Product"
    price: 99.99
    owner_id: $user_id
  }) {
    id
    name
    created_at
  }
}`,
    result: `{
  "products": [
    {
      "id": 205,
      "name": "New Product",
      "created_at": "2024-01-15T10:30:00Z"
    }
  ]
}`
  },
  {
    id: 'aggregation',
    label: 'Aggregation',
    graphql: `query {
  users {
    full_name
    products_count
    products_sum_price
    products_avg_price
  }
}`,
    result: `{
  "users": [
    {
      "full_name": "Bob Builder",
      "products_count": 12,
      "products_sum_price": 4500.50,
      "products_avg_price": 375.04
    }
  ]
}`
  },
  {
    id: 'subscription',
    label: 'Subscription',
    graphql: `subscription {
  products(where: { price: { gt: 50 } }) {
    id
    name
    price
    owner {
      full_name
    }
  }
}`,
    result: `{
  "products": [
    {
      "id": 104,
      "name": "Mechanical Keyboard",
      "price": 149.99,
      "owner": {
        "full_name": "Alice Dev"
      }
    }
  ]
}`
  }
];

export default function QueryShowcase() {
  const [activeId, setActiveId] = useState('query');
  const activeExample = examples.find(ex => ex.id === activeId) || examples[0];

  return (
    <section className="py-24" id="examples">
      <div className="mx-auto px-4">
        <div className="text-center mb-12">
          <h2 className="text-3xl md:text-5xl font-display font-bold text-gj-text mb-4">
            Simple & Powerful
          </h2>
          <p className="text-gj-muted text-lg max-w-xl mx-auto">
            Write GraphQL, get optimized SQL. Queries, mutations, subscriptions, and aggregations — all in one query.
          </p>
        </div>

        {/* Tabs */}
        <div className="flex flex-wrap justify-center gap-2 mb-8">
          {examples.map(ex => (
            <button
              type="button"
              key={ex.id}
              onClick={() => setActiveId(ex.id)}
              className={`px-5 py-2 rounded-lg text-sm font-medium transition-all duration-200
                ${activeId === ex.id
                  ? 'bg-gj-text text-gj-bg'
                  : 'bg-white/5 text-gj-muted hover:bg-white/10 hover:text-gj-text'}`}
            >
              {ex.label}
            </button>
          ))}
        </div>

        {/* Code Window */}
        <div className="bg-black rounded-2xl border border-white/10 overflow-hidden shadow-2xl">
          {/* Window Header */}
          <div className="bg-black/90 px-4 py-3 border-b border-white/10 flex items-center gap-2">
            <div className="w-3 h-3 rounded-full bg-[#FF5F56]" />
            <div className="w-3 h-3 rounded-full bg-[#FFBD2E]" />
            <div className="w-3 h-3 rounded-full bg-[#27C93F]" />
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 divide-y md:divide-y-0 md:divide-x divide-white/10">
            {/* Input */}
            <div className="p-6">
              <div className="flex items-center gap-2 text-xs text-white/40 mb-4 font-mono">
                <Terminal className="w-3 h-3" /> query.graphql
              </div>
              <pre className="text-sm text-purple-300 leading-relaxed overflow-x-auto">
                {activeExample.graphql}
              </pre>
            </div>

            {/* Output */}
            <div className="p-6 bg-black/20">
              <div className="flex items-center justify-between text-xs mb-4">
                <span className="flex items-center gap-2 text-white/40 font-mono">
                  <Database className="w-3 h-3" /> result.json
                </span>
                <span className="text-emerald-400 flex items-center gap-1">
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" />
                  18ms
                </span>
              </div>
              <pre className="text-sm text-emerald-300/80 leading-relaxed overflow-x-auto">
                {activeExample.result}
              </pre>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
