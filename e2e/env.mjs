// Fixed local ports and test-only secrets for the end-to-end run.
export const PORT = Number(process.env.E2E_PORT || 18080);
export const FAKES_PORT = Number(process.env.E2E_FAKES_PORT || 18099);
export const RENOXIUM = `http://localhost:${PORT}`;
export const FAKES = `http://127.0.0.1:${FAKES_PORT}`;
export const appURL = (app, preview = false) => `http://${app}${preview ? '--preview' : ''}.localhost:${PORT}`;
// Visitors sign in on a host of the platform's own, where nothing of the app runs.
export const idURL = (app, preview = false) => `http://${app}${preview ? '--preview' : ''}--id.localhost:${PORT}`;
export const OPERATOR = 'operator@e2e.test';
export const SECRETS = {
  webhook: 'whsec_e2e_connect',
  billingWebhook: 'whsec_e2e_billing',
  pricePro: 'price_e2e_pro',
  priceBusiness: 'price_e2e_business',
  priceWorkApp: 'price_e2e_work_app',
};
