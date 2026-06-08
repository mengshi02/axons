import { createContext, useContext, useState, useEffect, type ReactNode } from 'react';

type Theme = 'moon' | 'sun';

interface ThemeContextType {
  theme: Theme;
  setTheme: (theme: Theme) => void;
  toggleTheme: () => void;
}

const ThemeContext = createContext<ThemeContextType | null>(null);

const THEME_KEY = 'axons-theme';

// Get theme from localStorage or system preference
function getInitialTheme(): Theme {
  const stored = localStorage.getItem(THEME_KEY);
  if (stored === 'moon' || stored === 'sun') {
    return stored;
  }
  // Default to sun theme (light mode)
  return 'sun';
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>(getInitialTheme);

  // Apply theme class to document
  useEffect(() => {
    const root = document.documentElement;

    // Remove both classes first
    root.classList.remove('moon-theme', 'sun-theme');

    // Add the appropriate theme class
    if (theme === 'sun') {
      root.classList.add('sun-theme');
    } else {
      root.classList.add('moon-theme');
    }

    // Save to localStorage
    localStorage.setItem(THEME_KEY, theme);
  }, [theme]);

  // Listen for theme changes from other tabs/windows (same-origin sync via localStorage)
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === THEME_KEY && (e.newValue === 'moon' || e.newValue === 'sun')) {
        console.log('[ThemeProvider] storage event, new theme:', e.newValue);
        setThemeState(e.newValue as Theme);
      }
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  const setTheme = (newTheme: Theme) => {
    setThemeState(newTheme);
  };

  const toggleTheme = () => {
    setThemeState(prev => prev === 'moon' ? 'sun' : 'moon');
  };

  return (
    <ThemeContext.Provider value={{ theme, setTheme, toggleTheme }}>
      {children}
    </ThemeContext.Provider>
  );
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within ThemeProvider');
  }
  return context;
}