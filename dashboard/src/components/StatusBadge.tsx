interface StatusBadgeProps {
  status: string;
  label?: string;
}

function StatusBadge({ status, label }: StatusBadgeProps) {
  const config: Record<string, { bg: string; text: string; dot: string; animate?: boolean }> = {
    pending: {
      bg: "bg-gray-800",
      text: "text-gray-400",
      dot: "bg-gray-500",
    },
    processing: {
      bg: "bg-yellow-900/40",
      text: "text-yellow-300",
      dot: "bg-yellow-400",
      animate: true,
    },
    completed: {
      bg: "bg-green-900/40",
      text: "text-green-300",
      dot: "bg-green-400",
    },
    failed: {
      bg: "bg-red-900/40",
      text: "text-red-300",
      dot: "bg-red-400",
    },
  };

  const c = config[status] || config.pending;
  const displayText = label ? `${label}: ${status}` : status;

  return (
    <span
      className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${c.bg} ${c.text}`}
    >
      <span
        className={`w-1.5 h-1.5 rounded-full ${c.dot} ${c.animate ? "animate-pulse" : ""}`}
      />
      {displayText}
    </span>
  );
}

export default StatusBadge;
