const WINDOWS = [7, 14, 30, 90] as const;

interface Props {
  days: number;
  onChange: (days: number) => void;
  disabled?: boolean;
}

export default function NoisePills({ days, onChange, disabled = false }: Props) {
  return (
    <div className={`noise-pills${disabled ? ' noise-pills--disabled' : ''}`}>
      <span className="noise-pills__label">Noise window</span>
      <div className="noise-pills__group">
        {WINDOWS.map(w => (
          <button
            key={w}
            className={`noise-pills__pill${days === w ? ' noise-pills__pill--active' : ''}`}
            onClick={() => onChange(w)}
            disabled={disabled}
          >
            {w}d
          </button>
        ))}
      </div>
    </div>
  );
}
