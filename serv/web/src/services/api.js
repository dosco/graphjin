// Admin API client for GraphJin Web UI

const API_BASE = '/api/v1/admin';

// Helper to handle fetch responses with proper error handling
async function handleResponse(response) {
  let data;
  try {
    data = await response.json();
  } catch {
    // Network error or invalid JSON response
    if (!response.ok) {
      throw new Error(`Request failed: ${response.status} ${response.statusText}`);
    }
    throw new Error('Invalid response from server');
  }
  if (!response.ok) {
    throw new Error(data?.error || `Request failed: ${response.status}`);
  }
  return data;
}

// Wrapper to handle network errors
async function fetchWithErrorHandling(url) {
  try {
    const response = await fetch(url);
    return handleResponse(response);
  } catch (error) {
    if (error.name === 'TypeError' && error.message.includes('fetch')) {
      throw new Error('Network error: Unable to connect to server');
    }
    throw error;
  }
}

export const api = {
  // Tables
  getTables: () =>
    fetchWithErrorHandling(`${API_BASE}/tables`),

  getTableSchema: (name) =>
    fetchWithErrorHandling(`${API_BASE}/tables/${encodeURIComponent(name)}`),

  // Saved Queries
  getQueries: () =>
    fetchWithErrorHandling(`${API_BASE}/queries`),

  getQueryDetail: (name) =>
    fetchWithErrorHandling(`${API_BASE}/queries/${encodeURIComponent(name)}`),

  // Fragments
  getFragments: () =>
    fetchWithErrorHandling(`${API_BASE}/fragments`),

  // Configuration
  getConfig: () =>
    fetchWithErrorHandling(`${API_BASE}/config`),

  // Database Info (single database - legacy)
  getDatabase: () =>
    fetchWithErrorHandling(`${API_BASE}/database`),

  // Databases (multi-database support)
  getDatabases: () =>
    fetchWithErrorHandling(`${API_BASE}/databases`),
};

export default api;
