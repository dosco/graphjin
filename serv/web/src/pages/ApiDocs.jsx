import React, { Suspense, lazy } from "react";
import "swagger-ui-react/swagger-ui.css";

const SwaggerUI = lazy(() => import("swagger-ui-react"));

const ApiDocs = () => {
  return (
    <div className="gj-page gj-api-docs">
      <div className="gj-page-header">
        <h1 className="gj-page-title">API Documentation</h1>
        <p className="gj-page-subtitle">Interactive API reference and testing</p>
      </div>
      <div className="gj-swagger-container">
        <Suspense fallback={<div className="gj-loading">Loading API documentation...</div>}>
          <SwaggerUI url="/api/v1/openapi.json" />
        </Suspense>
      </div>
    </div>
  );
};

export default ApiDocs;
