import { useEffect, useState } from "react";

const PREFIX = "thura:demo:v1:";

// Demo data only. Backend credentials and real mailbox content do not belong here.
export function useStoredState(key, fallback) {
  const [value, setValue] = useState(() => {
    try {
      const raw = localStorage.getItem(PREFIX + key);
      if (raw === null) return fallback;
      const parsed = JSON.parse(raw);
      const valid = Array.isArray(fallback)
        ? Array.isArray(parsed)
        : typeof fallback === "object"
          ? parsed !== null &&
            typeof parsed === "object" &&
            !Array.isArray(parsed)
          : typeof parsed === typeof fallback;
      return valid ? parsed : fallback;
    } catch {
      return fallback;
    }
  });

  useEffect(() => {
    try {
      localStorage.setItem(PREFIX + key, JSON.stringify(value));
    } catch {
      // Private browsing or a full quota should not stop the demo from working.
    }
  }, [key, value]);

  return [value, setValue];
}
