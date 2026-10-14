const JSON_HEADERS = { 'Content-Type': 'application/json' };

function unauthorized() {
  const err = new Error('Not logged in.');
  err.status = 401;
  return err;
}

export async function getMe() {
  const res = await fetch('/api/auth/me');
  if (res.status === 401) return null;
  if (!res.ok) throw new Error('Could not check login status.');
  return res.json();
}

export async function register(username, password) {
  const res = await fetch('/api/auth/register', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ username, password }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Could not create account.');
  return data;
}

export async function login(username, password) {
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify({ username, password }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Could not log in.');
  return data;
}

export async function logout() {
  await fetch('/api/auth/logout', { method: 'POST' });
}

export async function getConfig() {
  const res = await fetch('/api/config');
  if (res.status === 401) throw unauthorized();
  if (!res.ok) throw new Error('Could not load settings.');
  return res.json();
}

export async function updateConfig(payload) {
  const res = await fetch('/api/config', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
  });
  if (res.status === 401) throw unauthorized();
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Could not save settings.');
  return data;
}

export async function getOllamaModels(host) {
  const query = host ? `?host=${encodeURIComponent(host)}` : '';
  const res = await fetch(`/api/ollama/models${query}`);
  if (res.status === 401) throw unauthorized();
  if (!res.ok) throw new Error('Could not load Ollama models.');
  return res.json();
}

export async function aggregate(payload, signal) {
  const res = await fetch('/api/aggregate', {
    method: 'POST',
    headers: JSON_HEADERS,
    body: JSON.stringify(payload),
    signal,
  });
  if (res.status === 401) throw unauthorized();
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Something went wrong.');
  return data;
}
