import { useCallback, useEffect, useRef, useState } from 'react';
import TagPicker from './components/TagPicker.jsx';
import WordSlider from './components/WordSlider.jsx';
import SummaryCard from './components/SummaryCard.jsx';
import SettingsDialog from './components/SettingsDialog.jsx';
import LoginScreen from './components/LoginScreen.jsx';
import { getMe, logout, getConfig, updateConfig, aggregate } from './api.js';

const DEBOUNCE_MS = 550;

export default function App() {
  const [date] = useState(() => new Date().toLocaleDateString(undefined, {
    weekday: 'long', year: 'numeric', month: 'long', day: 'numeric',
  }));

  const [authChecked, setAuthChecked] = useState(false);
  const [username, setUsername] = useState(null);

  const [settings, setSettings] = useState(null);
  const [availableTags, setAvailableTags] = useState([]);
  const [selectedTags, setSelectedTags] = useState([]);
  const [wordStops, setWordStops] = useState([]);
  const [stopIndex, setStopIndex] = useState(0);

  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  const [loading, setLoading] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);

  const requestSeq = useRef(0);
  const abortRef = useRef(null);
  const debounceRef = useRef(null);
  const initialLoad = useRef(true);

  useEffect(() => {
    getMe()
      .then((data) => {
        if (data) setUsername(data.username);
      })
      .finally(() => setAuthChecked(true));
  }, []);

  useEffect(() => {
    if (!username) return;
    getConfig()
      .then((data) => {
        setSettings(data);
        setAvailableTags(data.availableTags || []);
        setSelectedTags(data.tags && data.tags.length ? data.tags : ['World']);
        setWordStops(data.wordStops || []);
        setStopIndex(data.wordStopIndex || 0);
      })
      .catch((err) => {
        if (err.status === 401) {
          setUsername(null);
          return;
        }
        setError('Could not load settings from the server.');
      });
  }, [username]);

  const runAggregate = useCallback(async (tags, idx) => {
    if (tags.length === 0) {
      setLoading(false);
      return;
    }
    const seq = ++requestSeq.current;
    setLoading(true);
    setError(null);
    if (abortRef.current) abortRef.current.abort();
    const controller = new AbortController();
    abortRef.current = controller;

    try {
      const data = await aggregate(
        { tags, stopIndex: idx },
        controller.signal,
      );
      if (seq !== requestSeq.current) return;
      setResult(data);
    } catch (err) {
      if (err.name === 'AbortError' || seq !== requestSeq.current) return;
      if (err.status === 401) {
        setUsername(null);
        return;
      }
      setError(err.message || "Could not reach Short Report's server.");
    } finally {
      if (seq === requestSeq.current) setLoading(false);
    }
  }, []);

  // Regenerate whenever topics or the word target change. The very first
  // run (once settings load) fires immediately; later changes debounce so
  // dragging the slider doesn't spam the LLM.
  useEffect(() => {
    if (!settings) return;
    if (initialLoad.current) {
      initialLoad.current = false;
      runAggregate(selectedTags, stopIndex);
      return;
    }
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      runAggregate(selectedTags, stopIndex);
    }, DEBOUNCE_MS);
    return () => clearTimeout(debounceRef.current);
  }, [selectedTags, stopIndex, settings, runAggregate]);

  function toggleTag(tag) {
    setSelectedTags((prev) => {
      const key = tag.toLowerCase();
      const exists = prev.some((t) => t.toLowerCase() === key);
      return exists ? prev.filter((t) => t.toLowerCase() !== key) : [...prev, tag];
    });
  }

  function addTag(tag) {
    setSelectedTags((prev) => (
      prev.some((t) => t.toLowerCase() === tag.toLowerCase()) ? prev : [...prev, tag]
    ));
  }

  async function saveSettings(payload) {
    const data = await updateConfig(payload);
    setSettings(data);
    setSettingsOpen(false);
  }

  async function handleLogout() {
    await logout();
    setUsername(null);
    setSettings(null);
    setResult(null);
    setError(null);
    initialLoad.current = true;
  }

  if (!authChecked) {
    return null;
  }
  if (!username) {
    return <LoginScreen onAuthenticated={setUsername} />;
  }

  const providerLabel = settings
    ? settings.provider === 'ollama'
      ? `Local · ${settings.ollamaModel || 'Ollama'}`
      : settings.provider === 'openai'
        ? `OpenAI · ${settings.openaiModel || 'ChatGPT'}`
        : settings.provider === 'gemini'
          ? `Gemini · ${settings.geminiModel || 'Gemini'}`
          : `Anthropic · ${settings.anthropicModel || 'Claude'}`
    : '';

  return (
    <div className="page">
      <header className="hero">
        <p className="eyebrow">Short Report</p>
        <h1>{date}</h1>
      </header>

      <TagPicker
        availableTags={availableTags}
        selected={selectedTags}
        onToggle={toggleTag}
        onAdd={addTag}
      />

      <WordSlider stops={wordStops} index={stopIndex} onChange={setStopIndex} />

      <SummaryCard
        loading={loading}
        result={result}
        error={error}
        hasTags={selectedTags.length > 0}
      />

      <footer className="footer">
        <span className="provider-label">{username} · {providerLabel}</span>
        <span>
          <button
            type="button"
            className="icon-btn"
            aria-haspopup="dialog"
            onClick={() => setSettingsOpen(true)}
          >
            ⚙ Settings
          </button>
          <button type="button" className="icon-btn" onClick={handleLogout}>
            Log out
          </button>
        </span>
      </footer>

      <SettingsDialog
        open={settingsOpen}
        settings={settings}
        onClose={() => setSettingsOpen(false)}
        onSave={saveSettings}
      />
    </div>
  );
}
