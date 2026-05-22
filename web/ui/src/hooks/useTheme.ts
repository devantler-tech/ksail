import { useCallback, useEffect, useState } from "react";

export type Theme = "light" | "dark";

const STORAGE_KEY = "ksail-theme";

function prefersDark(): boolean {
  try {
    return window.matchMedia("(prefers-color-scheme: dark)").matches;
  } catch {
    return false;
  }
}

function initialTheme(): Theme {
  // localStorage access can throw in privacy mode, sandboxed frames, or when storage is blocked;
  // fall back to the system preference instead of crashing the initial render.
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "light" || stored === "dark") {
      return stored;
    }
  } catch {
    /* ignore: storage unavailable */
  }

  return prefersDark() ? "dark" : "light";
}

// useTheme tracks the active light/dark theme, persists the choice, and toggles the `dark` class on
// <html> so Tailwind's class-based dark variant applies. The no-flash init in index.html sets the
// initial class before paint; this keeps it in sync as the user toggles.
export function useTheme(): { theme: Theme; toggle: () => void } {
  const [theme, setTheme] = useState<Theme>(initialTheme);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    try {
      localStorage.setItem(STORAGE_KEY, theme);
    } catch {
      /* ignore: storage unavailable (theme still applies for this session) */
    }
  }, [theme]);

  const toggle = useCallback(() => {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  }, []);

  return { theme, toggle };
}
