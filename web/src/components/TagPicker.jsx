import { useState } from 'react';

export default function TagPicker({ availableTags, selected, onToggle, onAdd }) {
  const [draft, setDraft] = useState('');

  const merged = [...availableTags];
  for (const tag of selected) {
    if (!merged.some((t) => t.toLowerCase() === tag.toLowerCase())) merged.push(tag);
  }

  function handleSubmit(e) {
    e.preventDefault();
    const value = draft.trim();
    if (!value) return;
    onAdd(value);
    setDraft('');
  }

  return (
    <section className="tags" aria-label="News topics">
      <div className="tag-list">
        {merged.map((tag) => {
          const active = selected.some((s) => s.toLowerCase() === tag.toLowerCase());
          return (
            <button
              key={tag}
              type="button"
              className="tag-chip"
              aria-pressed={active}
              onClick={() => onToggle(tag)}
            >
              {tag}
            </button>
          );
        })}
      </div>
      <form className="add-tag" onSubmit={handleSubmit}>
        <input
          type="text"
          maxLength={40}
          placeholder="Add a topic…"
          aria-label="Add a topic"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
        <button type="submit" aria-label="Add topic">+</button>
      </form>
    </section>
  );
}
