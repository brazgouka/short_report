import { useState } from 'react';

function ArticleList({ articles }) {
  if (articles.length === 0) {
    return <p className="placeholder">No articles found for these topics right now.</p>;
  }
  const byTag = new Map();
  for (const a of articles) {
    if (!byTag.has(a.tag)) byTag.set(a.tag, []);
    byTag.get(a.tag).push(a);
  }
  return (
    <>
      {[...byTag.entries()].map(([tag, items]) => (
        <div key={tag}>
          <h3 className="article-group-title">{tag}</h3>
          <ul className="article-list">
            {items.map((a, i) => (
              <li key={a.link || i}>
                <a href={a.link} target="_blank" rel="noopener noreferrer">{a.title}</a>
                {a.description && <span className="article-desc"> — {a.description}</span>}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </>
  );
}

function BriefingItemRow({ item }) {
  const [expanded, setExpanded] = useState(false);
  const hasSources = item.sources && item.sources.length > 0;
  const canExpand = Boolean(item.detail) || hasSources;

  return (
    <li className="briefing-item">
      <button
        type="button"
        className="briefing-headline"
        aria-expanded={expanded}
        disabled={!canExpand}
        onClick={() => canExpand && setExpanded((e) => !e)}
      >
        <span>{item.headline}</span>
        {canExpand && <span className="chevron" aria-hidden="true">{expanded ? '−' : '+'}</span>}
      </button>
      {expanded && (
        <div className="briefing-detail">
          {item.detail && <p>{item.detail}</p>}
          {hasSources && (
            <ul className="briefing-sources">
              {item.sources.map((s, i) => (
                <li key={s.link || i}>
                  <a href={s.link} target="_blank" rel="noopener noreferrer">{s.title}</a>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </li>
  );
}

function BriefingItems({ items }) {
  const byTag = new Map();
  for (const item of items) {
    const tag = item.tag || 'Briefing';
    if (!byTag.has(tag)) byTag.set(tag, []);
    byTag.get(tag).push(item);
  }
  return (
    <>
      {[...byTag.entries()].map(([tag, tagItems]) => (
        <div key={tag}>
          <h3 className="article-group-title">{tag}</h3>
          <ul className="briefing-list">
            {tagItems.map((item, i) => (
              <BriefingItemRow key={i} item={item} />
            ))}
          </ul>
        </div>
      ))}
    </>
  );
}

function Body({ result, error, hasTags }) {
  if (error) return <p className="warning">{error}</p>;
  if (!hasTags) return <p className="placeholder">Select at least one topic to get today’s briefing.</p>;
  if (!result) {
    return <p className="placeholder">Pick your topics above and Short Report will pull together today's briefing.</p>;
  }

  return (
    <>
      {result.warning && <p className="warning">{result.warning}</p>}
      {result.unlimited ? (
        <ArticleList articles={result.articles || []} />
      ) : result.items && result.items.length > 0 ? (
        <BriefingItems items={result.items} />
      ) : (
        !result.warning && <p className="placeholder">Nothing to show yet.</p>
      )}
      <p className="meta">
        Updated {new Date(result.generatedAt).toLocaleTimeString()} ·{' '}
        {result.model ? `${result.provider} · ${result.model}` : 'raw headlines'}
      </p>
    </>
  );
}

export default function SummaryCard({ loading, result, error, hasTags }) {
  return (
    <section className="summary-wrap">
      <div className={`summary-card${loading ? ' is-loading' : ''}`}>
        <div className="updating-badge" aria-hidden="true">
          <span className="spinner" />Updating…
        </div>
        <div aria-live="polite">
          <Body result={result} error={error} hasTags={hasTags} />
        </div>
      </div>
    </section>
  );
}
