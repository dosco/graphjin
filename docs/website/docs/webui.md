---
id: webui
title: Web UI / GraphQL Editor
sidebar_label: Web UI
---

import useBaseUrl from '@docusaurus/useBaseUrl'; // Add to the top of the file below the front matter.

<img alt="Zipkin Traces" src={useBaseUrl("img/webui.jpg")} />

GraphJin comes with a build-in GraphQL editor that only runs in development. Use it to craft your queries and copy-paste them into you're app once you're ready. The editor supports auto-completation and schema documentation. This makes it super easy to craft and test your query all in one go without knowing anything about the underlying database structure.

You can even set query variables or http headers as required. To simulate an authenticated user set the http header `"X-USER-ID": 5` to the user id of the user you want to test with.
