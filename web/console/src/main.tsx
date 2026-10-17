import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { AgStudioProvider } from 'ag-studio-react';
import { AgStudioAiModule, enableStudioDevValidations } from 'ag-studio';
import { App } from './App';
import './styles.css';

if (import.meta.env.DEV) {
  // AG's guidance: development validations turn minified error codes into
  // readable messages naming the misconfigured property.
  enableStudioDevValidations();
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AgStudioProvider modules={[AgStudioAiModule]}>
      <App />
    </AgStudioProvider>
  </StrictMode>,
);
