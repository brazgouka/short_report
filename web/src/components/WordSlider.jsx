export default function WordSlider({ stops, index, onChange }) {
  if (stops.length === 0) return null;

  const stop = stops[index] ?? stops[0];
  const label = stop.infinite ? '∞ — Everything' : `${stop.label} word${stop.label === '1' ? '' : 's'}`;
  const lastIndex = stops.length - 1;
  const fillPercent = lastIndex === 0 ? 0 : (index / lastIndex) * 100;

  return (
    <section className="controls">
      <div className="controls-row">
        <label htmlFor="wordSlider">Briefing length</label>
        <span className="word-label">{label}</span>
      </div>
      <input
        id="wordSlider"
        type="range"
        min={0}
        max={lastIndex}
        step={1}
        value={index}
        style={{ '--fill': `${fillPercent}%` }}
        onChange={(e) => onChange(Number(e.target.value))}
      />
      <div className="slider-scale"><span>Terse</span><span>Everything</span></div>
    </section>
  );
}
