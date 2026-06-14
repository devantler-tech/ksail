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

// storedTheme reads the persisted choice. localStorage access can throw in privacy mode, sandboxed
// frames, or when storage is blocked; treat that as "no stored choice" instead of crashing.
function storedTheme(): Theme | null {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "light" || stored === "dark") {
      return stored;
    }
  } catch {
    /* ignore: storage unavailable */
  }

  return null;
}

// useTheme tracks the active light/dark theme and toggles the `dark` class on <html> so Tailwind's
// class-based dark variant applies. Until the user explicitly toggles, the theme follows the system
// preference live (including OS-level changes while the app is open); a toggle pins the choice and
// persists it. The no-flash init in index.html applies the same resolution before first paint.
export function useTheme(): { theme: Theme; toggle: () => void } {
  const [pinned, setPinned] = useState<boolean>(() => storedTheme() !== null);
  const [theme, setTheme] = useState<Theme>(() => storedTheme() ?? (prefersDark() ? "dark" : "light"));

  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
  }, [theme]);

  // Follow live OS theme changes while the user has not chosen explicitly.
  useEffect(() => {
    if (pinned) {
      return undefined;
    }

    try {
      const query = window.matchMedia("(prefers-color-scheme: dark)");
      const onChange = () => setTheme(query.matches ? "dark" : "light");
      query.addEventListener("change", onChange);

      return () => query.removeEventListener("change", onChange);
    } catch {
      return undefined; /* matchMedia unavailable: stay on the initial theme */
    }
  }, [pinned]);

  const toggle = useCallback(() => {
    setPinned(true);
    setTheme((current) => {
      const next = current === "dark" ? "light" : "dark";
      try {
        localStorage.setItem(STORAGE_KEY, next);
      } catch {
        /* ignore: storage unavailable (theme still applies for this session) */
      }

      return next;
    });
  }, []);

  return { theme, toggle };
}
