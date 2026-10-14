import { useEffect, useRef, useState } from 'react';
import { getOllamaModels } from '../api.js';

const PROVIDERS = [
  { value: 'anthropic', label: 'Claude' },
  { value: 'openai', label: 'ChatGPT' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'ollama', label: 'Local (Ollama)' },
];

function HostedProviderFields({
  idPrefix, label, modelPlaceholder,
  keyValue, keyFromEnv, keyConfigured, modelValue,
  onKeyChange, onModelChange,
}) {
  const keyHint = keyFromEnv
    ? 'A default key is provided by the server operator. Enter your own to override it.'
    : keyConfigured
      ? 'A key is already saved. Leave blank to keep it.'
      : 'No key saved yet.';

  return (
    <div className="provider-fields">
      <label htmlFor={`${idPrefix}Key`}>{label} API key</label>
      <input
        id={`${idPrefix}Key`}
        type="password"
        autoComplete="off"
        placeholder="Paste your API key"
        value={keyValue}
        onChange={(e) => onKeyChange(e.target.value)}
      />
      <p className="hint">{keyHint}</p>
      <label htmlFor={`${idPrefix}Model`}>Model</label>
      <input
        id={`${idPrefix}Model`}
        type="text"
        placeholder={modelPlaceholder}
        value={modelValue}
        onChange={(e) => onModelChange(e.target.value)}
      />
    </div>
  );
}

export default function SettingsDialog({ open, settings, onClose, onSave }) {
  const dialogRef = useRef(null);
  const [form, setForm] = useState(null);
  const [error, setError] = useState('');
  const [banDraft, setBanDraft] = useState('');

  const [ollamaModels, setOllamaModels] = useState([]);
  const [modelsLoading, setModelsLoading] = useState(false);
  const [modelsError, setModelsError] = useState('');
  const modelsSeq = useRef(0);

  async function refreshModels(host) {
    const seq = ++modelsSeq.current;
    setModelsLoading(true);
    setModelsError('');
    try {
      const data = await getOllamaModels(host);
      if (seq !== modelsSeq.current) return;
      setOllamaModels(data.models || []);
      if (data.error) setModelsError(data.error);
    } catch (err) {
      if (seq !== modelsSeq.current) return;
      setOllamaModels([]);
      setModelsError(err.message || 'Could not load models.');
    } finally {
      if (seq === modelsSeq.current) setModelsLoading(false);
    }
  }

  useEffect(() => {
    if (open && settings) {
      setForm({
        provider: settings.provider,
        anthropicKey: '',
        anthropicModel: settings.anthropicModel || '',
        openaiKey: '',
        openaiModel: settings.openaiModel || '',
        geminiKey: '',
        geminiModel: settings.geminiModel || '',
        ollamaHost: settings.ollamaHost || '',
        ollamaModel: settings.ollamaModel || '',
        bannedSources: settings.bannedSources || [],
      });
      setError('');
      setBanDraft('');
    }
  }, [open, settings]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    const handleCancel = () => onClose();
    dialog.addEventListener('cancel', handleCancel);
    return () => dialog.removeEventListener('cancel', handleCancel);
  }, [onClose]);

  useEffect(() => {
    if (!open || !form || form.provider !== 'ollama') return;
    const t = setTimeout(() => refreshModels(form.ollamaHost), 400);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, form?.provider, form?.ollamaHost]);

  if (!settings || !form) {
    return <dialog ref={dialogRef} />;
  }

  function addBanned() {
    const d = banDraft.trim().toLowerCase();
    if (!d) return;
    if (!form.bannedSources.includes(d)) {
      setForm({ ...form, bannedSources: [...form.bannedSources, d] });
    }
    setBanDraft('');
  }

  function removeBanned(domain) {
    setForm({ ...form, bannedSources: form.bannedSources.filter((d) => d !== domain) });
  }

  async function handleSubmit(e) {
    e.preventDefault();
    const payload = {
      provider: form.provider,
      anthropicModel: form.anthropicModel.trim(),
      openaiModel: form.openaiModel.trim(),
      geminiModel: form.geminiModel.trim(),
      ollamaHost: form.ollamaHost.trim(),
      ollamaModel: form.ollamaModel.trim(),
      bannedSources: form.bannedSources,
    };
    if (form.anthropicKey.trim()) payload.anthropicKey = form.anthropicKey.trim();
    if (form.openaiKey.trim()) payload.openaiKey = form.openaiKey.trim();
    if (form.geminiKey.trim()) payload.geminiKey = form.geminiKey.trim();

    try {
      await onSave(payload);
    } catch (err) {
      setError(err.message || 'Could not save settings.');
    }
  }

  return (
    <dialog ref={dialogRef} aria-labelledby="settingsTitle">
      <form method="dialog" onSubmit={handleSubmit}>
        <h2 id="settingsTitle">Settings</h2>

        <div className="settings-section">
          <fieldset>
            <legend>LLM provider</legend>
            <div className="tag-list">
              {PROVIDERS.map((p) => (
                <button
                  key={p.value}
                  type="button"
                  className="tag-chip"
                  aria-pressed={form.provider === p.value}
                  onClick={() => setForm({ ...form, provider: p.value })}
                >
                  {p.label}
                </button>
              ))}
            </div>
          </fieldset>

          {form.provider === 'anthropic' && (
            <HostedProviderFields
              idPrefix="anthropic" label="Anthropic" modelPlaceholder="claude-sonnet-5"
              keyValue={form.anthropicKey}
              keyFromEnv={settings.anthropicKeyFromEnv}
              keyConfigured={settings.anthropicKeyConfigured}
              modelValue={form.anthropicModel}
              onKeyChange={(v) => setForm({ ...form, anthropicKey: v })}
              onModelChange={(v) => setForm({ ...form, anthropicModel: v })}
            />
          )}

          {form.provider === 'openai' && (
            <HostedProviderFields
              idPrefix="openai" label="OpenAI" modelPlaceholder="gpt-4o"
              keyValue={form.openaiKey}
              keyFromEnv={settings.openaiKeyFromEnv}
              keyConfigured={settings.openaiKeyConfigured}
              modelValue={form.openaiModel}
              onKeyChange={(v) => setForm({ ...form, openaiKey: v })}
              onModelChange={(v) => setForm({ ...form, openaiModel: v })}
            />
          )}

          {form.provider === 'gemini' && (
            <HostedProviderFields
              idPrefix="gemini" label="Gemini" modelPlaceholder="gemini-2.0-flash"
              keyValue={form.geminiKey}
              keyFromEnv={settings.geminiKeyFromEnv}
              keyConfigured={settings.geminiKeyConfigured}
              modelValue={form.geminiModel}
              onKeyChange={(v) => setForm({ ...form, geminiKey: v })}
              onModelChange={(v) => setForm({ ...form, geminiModel: v })}
            />
          )}

          {form.provider === 'ollama' && (
            <div className="provider-fields">
              <label htmlFor="ollamaHost">Server URL</label>
              <input
                id="ollamaHost"
                type="text"
                placeholder="http://localhost:11434"
                value={form.ollamaHost}
                onChange={(e) => setForm({ ...form, ollamaHost: e.target.value })}
              />

              <div className="label-row">
                <label htmlFor="ollamaModel">Model</label>
                <button
                  type="button"
                  className="link-btn"
                  disabled={modelsLoading}
                  onClick={() => refreshModels(form.ollamaHost)}
                >
                  {modelsLoading ? 'Refreshing…' : '↻ Refresh'}
                </button>
              </div>

              {ollamaModels.length > 0 ? (
                <select
                  id="ollamaModel"
                  value={form.ollamaModel}
                  onChange={(e) => setForm({ ...form, ollamaModel: e.target.value })}
                >
                  {form.ollamaModel && !ollamaModels.includes(form.ollamaModel) && (
                    <option value={form.ollamaModel}>{form.ollamaModel} (not detected)</option>
                  )}
                  {ollamaModels.map((m) => (
                    <option key={m} value={m}>{m}</option>
                  ))}
                </select>
              ) : (
                <>
                  <input
                    id="ollamaModel"
                    type="text"
                    placeholder="llama3.2"
                    value={form.ollamaModel}
                    onChange={(e) => setForm({ ...form, ollamaModel: e.target.value })}
                  />
                  <p className="hint">
                    {modelsLoading
                      ? 'Looking for local models…'
                      : modelsError
                        ? `${modelsError} — enter a model name manually, or pull one and refresh.`
                        : 'No local models detected — enter a name manually, or pull one and refresh.'}
                  </p>
                </>
              )}
            </div>
          )}
        </div>

        <div className="settings-section">
          <fieldset>
            <legend>Blocked sources</legend>
            {form.bannedSources.length > 0 && (
              <div className="tag-list">
                {form.bannedSources.map((d) => (
                  <button
                    key={d}
                    type="button"
                    className="tag-chip"
                    aria-pressed="true"
                    title="Remove"
                    onClick={() => removeBanned(d)}
                  >
                    {d} ✕
                  </button>
                ))}
              </div>
            )}
            <div className="add-tag">
              <input
                type="text"
                placeholder="e.g. example.com"
                value={banDraft}
                onChange={(e) => setBanDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    addBanned();
                  }
                }}
              />
              <button type="button" onClick={addBanned} aria-label="Block source">+</button>
            </div>
            <p className="hint">News from these sites will never be fetched or shown.</p>
          </fieldset>
        </div>

        {error && <p className="settings-error" role="alert">{error}</p>}

        <div className="dialog-actions">
          <button type="button" className="btn-secondary" onClick={onClose}>Cancel</button>
          <button type="submit" className="btn-primary">Save</button>
        </div>
      </form>
    </dialog>
  );
}
