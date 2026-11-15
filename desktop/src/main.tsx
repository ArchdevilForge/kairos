import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './styles/anthropic.css';
import { App } from './App';
import { I18nProvider } from './i18n';

const rootEl = document.getElementById('root');
if (rootEl) {
  createRoot(rootEl).render(
    <StrictMode>
      <I18nProvider>
        <App />
      </I18nProvider>
    </StrictMode>,
  );
}
