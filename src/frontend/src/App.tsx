import { useEffect, useState, type FormEvent } from 'react';

type Plan = 'starter' | 'hq' | 'none';
type ViewMode = 'home' | 'subscribe' | 'docs';

type BillingStatus = 'inactive' | 'pending' | 'active';

type PaymentSummary = {
  id: string;
  status: string;
  amount: number;
  currency: string;
};

type SubscriptionSummary = {
  id: string;
  status: string;
  planCode: string;
};

type AccountState = {
  name: string;
  email: string;
  plan: Plan;
  billingStatus: BillingStatus;
  trialActive: boolean;
  isAuthenticated: boolean;
  checkoutUrl: string;
  lastWebhookEvent: string;
  failureReason: string;
  lastWebhookAt: string;
  payments: PaymentSummary[];
  subscriptions: SubscriptionSummary[];
};

const emptyState: AccountState = {
  name: '',
  email: '',
  plan: 'none',
  billingStatus: 'inactive',
  trialActive: false,
  isAuthenticated: false,
  checkoutUrl: '',
  lastWebhookEvent: '',
  failureReason: '',
  lastWebhookAt: '',
  payments: [],
  subscriptions: [],
};

const DRAFT_STORAGE_KEY = 'sop-cloud-site-draft';
const ACCOUNT_STATE_STORAGE_KEY = 'sop-cloud-site-account-state';

type PersistedAccountState = {
  name: string;
  email: string;
  plan: Plan;
  billingStatus: BillingStatus;
  isAuthenticated: boolean;
  checkoutUrl: string;
  lastWebhookEvent: string;
  failureReason: string;
  lastWebhookAt: string;
  payments: PaymentSummary[];
  subscriptions: SubscriptionSummary[];
};

function loadDraftState(): { name: string; email: string } {
  if (typeof window === 'undefined') {
    return { name: '', email: '' };
  }
  try {
    const raw = window.localStorage.getItem(DRAFT_STORAGE_KEY);
    if (!raw) {
      return { name: '', email: '' };
    }
    const parsed = JSON.parse(raw) as Partial<{ name: string; email: string }>;
    return {
      name: parsed.name ?? '',
      email: parsed.email ?? '',
    };
  } catch {
    return { name: '', email: '' };
  }
}

function loadPersistedAccountState(): PersistedAccountState | null {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    const raw = window.localStorage.getItem(ACCOUNT_STATE_STORAGE_KEY);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as Partial<PersistedAccountState>;
    if (!parsed || typeof parsed !== 'object') {
      return null;
    }
    return {
      name: parsed.name ?? '',
      email: parsed.email ?? '',
      plan: normalizePlan(parsed.plan),
      billingStatus: normalizeBillingStatus(parsed.billingStatus),
      isAuthenticated: Boolean(parsed.isAuthenticated),
      checkoutUrl: parsed.checkoutUrl ?? '',
      lastWebhookEvent: parsed.lastWebhookEvent ?? '',
      failureReason: parsed.failureReason ?? '',
      lastWebhookAt: parsed.lastWebhookAt ?? '',
      payments: Array.isArray(parsed.payments) ? parsed.payments : [],
      subscriptions: Array.isArray(parsed.subscriptions) ? parsed.subscriptions : [],
    };
  } catch {
    return null;
  }
}

function persistAccountState(account: AccountState) {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(
    ACCOUNT_STATE_STORAGE_KEY,
    JSON.stringify({
      name: account.name,
      email: account.email,
      plan: account.plan,
      billingStatus: account.billingStatus,
      isAuthenticated: account.isAuthenticated,
      checkoutUrl: account.checkoutUrl,
      lastWebhookEvent: account.lastWebhookEvent,
      failureReason: account.failureReason,
      lastWebhookAt: account.lastWebhookAt,
      payments: account.payments,
      subscriptions: account.subscriptions,
    })
  );
}

function resolveView(pathname: string): ViewMode {
  if (pathname.startsWith('/site/subscribe')) {
    return 'subscribe';
  }
  if (pathname.startsWith('/site/docs')) {
    return 'docs';
  }
  return 'home';
}

function normalizePlan(plan: string | undefined): Plan {
  if (!plan) {
    return 'none';
  }
  const normalized = plan.toLowerCase();
  if (normalized.includes('starter')) {
    return 'starter';
  }
  if (normalized.includes('hq')) {
    return 'hq';
  }
  return 'none';
}

function normalizeBillingStatus(value: string | undefined): BillingStatus {
  if (value === 'active') {
    return 'active';
  }
  if (value === 'pending') {
    return 'pending';
  }
  return 'inactive';
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 24) || 'sop-user';
}

function buildUsername(name: string, email: string): string {
  const base = slugify(name || email || 'sop-user');
  const suffix = (email || '').toLowerCase().replace(/[^a-z0-9]+/g, '');
  return suffix ? `${base}-${suffix.slice(0, 8)}` : base;
}

function buildPassword(email: string): string {
  return `sop-${(email || '').trim().toLowerCase().replace(/[^a-z0-9]+/g, '-') || 'site'}`;
}

async function fetchJSON(url: string, init: RequestInit = {}): Promise<any> {
  const response = await fetch(url, {
    credentials: 'include',
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init.headers || {}),
    },
  });

  const text = await response.text();
  let payload: any = null;
  if (text) {
    try {
      payload = JSON.parse(text);
    } catch {
      payload = null;
    }
  }

  if (!response.ok) {
    const message = payload?.message || payload?.error || 'request failed';
    throw new Error(message);
  }

  return payload;
}

async function loadAccountFromServer(): Promise<AccountState> {
  const response = await fetch('/api/auth/status', { credentials: 'include' });
  if (!response.ok) {
    return emptyState;
  }
  const payload = await response.json();
  const user = payload?.user ?? {};
  const plan = normalizePlan(user.subscription_plan);
  const billingStatus = normalizeBillingStatus(user.billing_status);
  const billing = user.billing ?? {};
  return {
    name: user.username ?? '',
    email: user.email ?? '',
    plan,
    billingStatus,
    trialActive: billingStatus === 'active' && plan === 'starter',
    isAuthenticated: true,
    checkoutUrl: billing.checkout_url ?? '',
    lastWebhookEvent: billing.last_webhook_event ?? '',
    failureReason: billing.failure_reason ?? '',
    lastWebhookAt: billing.last_webhook_at ?? '',
    payments: Array.isArray(user.payments) ? user.payments : [],
    subscriptions: Array.isArray(user.subscriptions) ? user.subscriptions : [],
  };
}

async function ensureAuthenticatedAccount(name: string, email: string): Promise<AccountState> {
  const username = buildUsername(name, email);
  const password = buildPassword(email);

  const loginResponse = await fetch('/api/site/auth/login', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });

  if (!loginResponse.ok && loginResponse.status !== 401) {
    throw new Error('Unable to sign in to your account.');
  }

  if (!loginResponse.ok) {
    const registerResponse = await fetch('/api/auth/register', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        username,
        password,
        email,
        role: 'user',
        subscription_plan: 'starter',
      }),
    });

    if (!registerResponse.ok && registerResponse.status !== 409) {
      throw new Error('Unable to create your account.');
    }

    const followUpLoginResponse = await fetch('/api/site/auth/login', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    if (!followUpLoginResponse.ok) {
      throw new Error('Unable to sign in after account creation.');
    }
  }

  return loadAccountFromServer();
}

async function updateSubscriptionPlan(plan: Plan): Promise<AccountState> {
  await fetchJSON('/api/auth/subscription', {
    method: 'POST',
    body: JSON.stringify({ plan }),
  });
  return loadAccountFromServer();
}

async function createCheckoutSession(plan: Plan): Promise<{ url: string; status: string }> {
  const payload = await fetchJSON('/api/auth/checkout', {
    method: 'POST',
    body: JSON.stringify({ plan }),
  });
  return {
    url: payload?.checkout?.url || '',
    status: payload?.checkout?.status || 'pending',
  };
}

async function downloadLocalAgent(): Promise<void> {
  const payload = await fetchJSON('/api/auth/download');
  const downloadURL = payload?.download_url || '';
  if (!payload?.available || !downloadURL) {
    throw new Error('Your download is not available yet.');
  }
  if (typeof window !== 'undefined') {
    window.open(downloadURL, '_blank', 'noopener,noreferrer');
  }
}

async function loginWithCredentials(username: string, password: string): Promise<AccountState> {
  const response = await fetch('/api/site/auth/login', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  if (!response.ok) {
    throw new Error('Unable to sign in to your account.');
  }
  return loadAccountFromServer();
}

async function logoutFromServer(): Promise<void> {
  await fetch('/api/site/auth/logout', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
  });
}

export default function App() {
  const [account, setAccount] = useState<AccountState>(emptyState);
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [showInstallStep, setShowInstallStep] = useState(false);
  const [view, setView] = useState<ViewMode>(() => resolveView(typeof window === 'undefined' ? '/' : window.location.pathname));
  const [cardName, setCardName] = useState('');
  const [cardNumber, setCardNumber] = useState('');
  const [cardExpiry, setCardExpiry] = useState('');
  const [cardCVC, setCardCVC] = useState('');
  const [checkoutComplete, setCheckoutComplete] = useState(false);
  const [statusMessage, setStatusMessage] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    const draft = loadDraftState();
    const persisted = loadPersistedAccountState();
    setName(draft.name);
    setEmail(draft.email);
    if (persisted && persisted.isAuthenticated) {
      const restored: AccountState = {
        ...emptyState,
        ...persisted,
        trialActive: persisted.billingStatus === 'active' && persisted.plan === 'starter',
      };
      setAccount(restored);
      setName(restored.name || draft.name);
      setEmail(restored.email || draft.email);
      setShowInstallStep(restored.billingStatus === 'active' && restored.plan === 'starter');
      setCheckoutComplete(restored.billingStatus === 'active' && restored.plan === 'starter');
    }
    void (async () => {
      try {
        const nextAccount = await loadAccountFromServer();
        const resolvedAccount: AccountState = {
          ...nextAccount,
          name: nextAccount.name || persisted?.name || draft.name,
          email: nextAccount.email || persisted?.email || draft.email,
          trialActive: nextAccount.billingStatus === 'active' && nextAccount.plan === 'starter',
        };
        setAccount(resolvedAccount);
        if (resolvedAccount.isAuthenticated) {
          setName(resolvedAccount.name || draft.name);
          setEmail(resolvedAccount.email || draft.email);
          setShowInstallStep(resolvedAccount.billingStatus === 'active' && resolvedAccount.plan === 'starter');
          setCheckoutComplete(resolvedAccount.billingStatus === 'active' && resolvedAccount.plan === 'starter');
          persistAccountState(resolvedAccount);
        }
      } catch {
        setAccount(emptyState);
      }
    })();
  }, []);

  useEffect(() => {
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(DRAFT_STORAGE_KEY, JSON.stringify({ name, email }));
    }
  }, [name, email]);

  useEffect(() => {
    if (account.isAuthenticated) {
      persistAccountState(account);
    }
  }, [account]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }
    const onPopState = () => {
      setView(resolveView(window.location.pathname));
    };
    window.addEventListener('popstate', onPopState);
    return () => window.removeEventListener('popstate', onPopState);
  }, []);

  const handleStart = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!name.trim() || !email.trim()) {
      return;
    }

    setIsSubmitting(true);
    setStatusMessage('');
    try {
      const nextAccount = await ensureAuthenticatedAccount(name.trim(), email.trim());
      const accountWithState = {
        ...nextAccount,
        name: name.trim(),
        email: email.trim(),
        plan: nextAccount.plan || 'starter',
        billingStatus: nextAccount.billingStatus || 'pending',
        trialActive: nextAccount.billingStatus === 'active',
        isAuthenticated: true,
        checkoutUrl: nextAccount.checkoutUrl || '',
        lastWebhookEvent: nextAccount.lastWebhookEvent || '',
        failureReason: nextAccount.failureReason || '',
        lastWebhookAt: nextAccount.lastWebhookAt || '',
      };
      setAccount(accountWithState);
      setShowInstallStep(accountWithState.billingStatus === 'active');
      setView('home');
      setStatusMessage(
        accountWithState.billingStatus === 'active'
          ? 'Your account is now connected to the backend and your starter plan is active.'
          : 'Your account is connected. Your starter subscription is pending payment confirmation.'
      );
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to sync your account.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleSubscription = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!cardName.trim() || !cardNumber.trim() || !cardExpiry.trim() || !cardCVC.trim()) {
      return;
    }

    setIsSubmitting(true);
    setStatusMessage('');
    try {
      const nextAccount = await ensureAuthenticatedAccount(account.name || name.trim(), account.email || email.trim());
      if (nextAccount.billingStatus === 'pending' && nextAccount.checkoutUrl) {
        setAccount({
          ...nextAccount,
          name: nextAccount.name || account.name || name.trim(),
          email: nextAccount.email || account.email || email.trim(),
          plan: 'starter',
          billingStatus: 'pending',
          trialActive: false,
          isAuthenticated: true,
        });
        setShowInstallStep(false);
        setCheckoutComplete(false);
        setStatusMessage('Your checkout is still pending. We are taking you back to the payment page.');
        window.location.assign(nextAccount.checkoutUrl);
        return;
      }
      const checkout = await createCheckoutSession('starter');
      if (checkout.url) {
        setAccount({
          ...nextAccount,
          name: nextAccount.name || account.name || name.trim(),
          email: nextAccount.email || account.email || email.trim(),
          plan: 'starter',
          billingStatus: 'pending',
          trialActive: false,
          isAuthenticated: true,
          checkoutUrl: checkout.url,
          lastWebhookEvent: '',
          failureReason: '',
          lastWebhookAt: '',
        });
        setShowInstallStep(false);
        setCheckoutComplete(false);
        setStatusMessage('We are sending you to checkout. Your starter plan will activate after payment confirmation.');
        window.location.assign(checkout.url);
        return;
      }
      const synced = await updateSubscriptionPlan('starter');
      setAccount({
        ...nextAccount,
        name: nextAccount.name || account.name || name.trim(),
        email: nextAccount.email || account.email || email.trim(),
        plan: synced.plan,
        billingStatus: synced.billingStatus,
        trialActive: synced.billingStatus === 'active',
        isAuthenticated: true,
        checkoutUrl: '',
        lastWebhookEvent: '',
        failureReason: '',
        lastWebhookAt: '',
      });
      setShowInstallStep(synced.billingStatus === 'active');
      setCheckoutComplete(synced.billingStatus === 'active');
      setStatusMessage(
        synced.billingStatus === 'active'
          ? 'Subscription confirmed. Your starter plan is now active.'
          : 'Your starter subscription is pending confirmation.'
      );
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to complete the subscription.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleUnsubscribe = async () => {
    setIsSubmitting(true);
    setStatusMessage('');
    try {
      const synced = await updateSubscriptionPlan('none');
      setAccount((current) => ({
        ...current,
        ...synced,
        plan: 'none',
        billingStatus: 'inactive',
        trialActive: false,
        isAuthenticated: true,
        checkoutUrl: '',
        lastWebhookEvent: '',
        failureReason: '',
        lastWebhookAt: '',
      }));
      setCheckoutComplete(false);
      setStatusMessage('You have been unsubscribed from the starter plan.');
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to unsubscribe right now.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleUnregister = async () => {
    setIsSubmitting(true);
    setStatusMessage('');
    try {
      await fetchJSON('/api/auth/unregister', { method: 'POST' });
      setAccount(emptyState);
      if (typeof window !== 'undefined') {
        window.localStorage.removeItem(DRAFT_STORAGE_KEY);
      }
      setName('');
      setEmail('');
      setCardName('');
      setCardNumber('');
      setCardExpiry('');
      setCardCVC('');
      setCheckoutComplete(false);
      setShowInstallStep(false);
      setStatusMessage('Your account has been removed.');
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to remove the account.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleLogin = async () => {
    if (!name.trim() || !email.trim()) {
      return;
    }
    setIsSubmitting(true);
    setStatusMessage('');
    try {
      const username = buildUsername(name.trim(), email.trim());
      const password = buildPassword(email.trim());
      const synced = await loginWithCredentials(username, password);
      const resolvedAccount = {
        ...synced,
        name: synced.name || name.trim(),
        email: synced.email || email.trim(),
        trialActive: synced.billingStatus === 'active' && synced.plan === 'starter',
      };
      setAccount(resolvedAccount);
      setShowInstallStep(resolvedAccount.billingStatus === 'active' && resolvedAccount.plan === 'starter');
      setCheckoutComplete(resolvedAccount.billingStatus === 'active' && resolvedAccount.plan === 'starter');
      setStatusMessage(resolvedAccount.isAuthenticated ? 'Signed in successfully.' : 'Signed in.');
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to sign in right now.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleDownload = async () => {
    setIsSubmitting(true);
    setStatusMessage('');
    try {
      await downloadLocalAgent();
      setStatusMessage('Opening the local-agent download link.');
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to start the download right now.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleLogout = async () => {
    setIsSubmitting(true);
    setStatusMessage('');
    try {
      await logoutFromServer();
      setAccount(emptyState);
      setName('');
      setEmail('');
      setCardName('');
      setCardNumber('');
      setCardExpiry('');
      setCardCVC('');
      setCheckoutComplete(false);
      setShowInstallStep(false);
      setStatusMessage('You have been signed out.');
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to sign out right now.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const isStarterActive = account.billingStatus === 'active' && account.plan === 'starter';
  const isStarterPending = account.billingStatus === 'pending' && account.plan === 'starter';
  const shouldShowInstallPanel = showInstallStep || isStarterActive || checkoutComplete;
  const canResumeCheckout = Boolean(account.checkoutUrl) && (account.billingStatus === 'pending' || Boolean(account.failureReason));
  const billingStatusTitle = account.billingStatus === 'active'
    ? 'Subscription confirmed'
    : account.billingStatus === 'pending'
      ? 'Checkout pending'
      : account.failureReason
        ? 'Checkout needs attention'
        : 'Not subscribed';
  const billingStatusBody = account.billingStatus === 'active'
    ? 'Your Starter Kit is active and ready to use.'
    : account.billingStatus === 'pending'
      ? 'Your subscription is waiting for payment confirmation.'
      : account.failureReason
        ? `The last checkout ended with: ${account.failureReason}.` 
        : 'No active subscription is currently in place.';

  if (view === 'subscribe') {
    return (
      <main className="app-shell">
        <section className="hero-card subscription-shell">
          <div className="hero-actions">
            <a className="button button-secondary" href="/site/">Back to overview</a>
            <a className="button button-secondary" href="/site/docs">View product docs</a>
            <a className="button button-primary" href="/app">Open Data Manager</a>
          </div>

          <div className="checkout-grid">
            <div className="checkout-card">
              <p className="eyebrow">Secure checkout</p>
              <h2>Start your Northstar Starter Kit subscription</h2>
              <p className="muted-copy">
                Your card is handled securely and we only need the minimum information required to start the monthly subscription. An active subscription unlocks licensed use of the Northstar AI polyglot library in the supported runtimes, including the bundled service-side experiences that enforce subscription-bound security tokens.
              </p>
              <p className="muted-copy" style={{ marginTop: '0.75rem' }}>
                Visit <strong>ai-northstar.com</strong> for the public product site and onboarding experience.
              </p>

              {canResumeCheckout ? (
                <div className="preview-box">
                  <h4>{account.failureReason ? 'Resume or retry checkout' : 'Resume checkout'}</h4>
                  <p>{account.failureReason ? `Your previous checkout ended with: ${account.failureReason}.` : 'Your checkout is still waiting for completion. Continue where you left off.'}</p>
                  <button type="button" className="button button-secondary" onClick={() => { if (account.checkoutUrl) window.location.assign(account.checkoutUrl); }} disabled={isSubmitting}>
                    Resume checkout
                  </button>
                </div>
              ) : null}

              <form className="form-stack" onSubmit={handleSubscription}>
                <label>
                  Cardholder name
                  <input value={cardName} onChange={(event) => setCardName(event.target.value)} placeholder="Alex Rivera" />
                </label>
                <label>
                  Card number
                  <input value={cardNumber} onChange={(event) => setCardNumber(event.target.value)} placeholder="4242 4242 4242 4242" />
                </label>
                <div className="inline-fields">
                  <label>
                    Expiry
                    <input value={cardExpiry} onChange={(event) => setCardExpiry(event.target.value)} placeholder="12/30" />
                  </label>
                  <label>
                    CVC
                    <input value={cardCVC} onChange={(event) => setCardCVC(event.target.value)} placeholder="123" />
                  </label>
                </div>
                <button type="submit" className="button button-primary" disabled={isSubmitting}>
                  {isSubmitting ? 'Working…' : 'Complete subscription'}
                </button>
              </form>
            </div>

            <div className="checkout-card summary-card">
              <h3>Order summary</h3>
              <div className="summary-list">
                <div>
                  <span className="summary-label">Plan</span>
                  <strong>Starter Kit</strong>
                </div>
                <div>
                  <span className="summary-label">Billing</span>
                  <strong>$5/month</strong>
                </div>
                <div>
                  <span className="summary-label">Includes</span>
                  <strong>Local agent access</strong>
                </div>
              </div>

              {statusMessage ? <p className="muted-copy">{statusMessage}</p> : null}

              {account.isAuthenticated ? (
                <div className="preview-box">
                  <h4>{billingStatusTitle}</h4>
                  <p>{billingStatusBody}</p>
                </div>
              ) : null}

              {checkoutComplete ? (
                <div className="success-banner">
                  <h4>Subscription confirmed</h4>
                  <p>Your Starter Kit is active, your access to the Northstar AI polyglot library is now enabled, and the local-agent download step is ready.</p>
                  <p className="muted-copy" style={{ marginTop: '0.75rem' }}>Public site: <strong>ai-northstar.com</strong></p>
                </div>
              ) : (
                <div className="preview-box">
                  <h4>What happens next</h4>
                  <p>After checkout, we guide you to install the local agent and continue with the onboarding flow.</p>
                </div>
              )}

              {shouldShowInstallPanel ? (
                <div className="preview-box install-box">
                  <h4>Install the local agent</h4>
                  <p>Download the local agent from the official SOP releases page to run the host-side workflow on your own machine. The agent pairs with the AI copilot so you can move from setup to guided execution.</p>
                  <button type="button" className="button button-secondary" onClick={handleDownload} disabled={isSubmitting}>
                    {isSubmitting ? 'Preparing…' : 'Download local agent'}
                  </button>
                </div>
              ) : null}
            </div>
          </div>
        </section>
      </main>
    );
  }

  if (view === 'docs') {
    return (
      <main className="app-shell">
        <section className="hero-card docs-shell">
          <div className="hero-actions">
            <a className="button button-secondary" href="/site/">Back to overview</a>
            <a className="button button-primary" href="/site/subscribe">Start Starter Kit</a>
          </div>

          <div className="docs-intro">
            <p className="eyebrow">Product documentation</p>
            <h1 className="hero-title">Learn how Northstar brings local workflows, knowledge bases, and AI guidance together.</h1>
            <p className="hero-copy">
              The new site is built for self-service onboarding. It walks users from account creation and plan selection to local-agent installation and the first AI-guided workflow. An active subscription unlocks licensed use of the Northstar AI polyglot library across supported runtimes, while the bundled httpserver and daemon enforce that subscription-bound access for service-side use.
            </p>
            <p className="hero-copy" style={{ marginTop: '1rem' }}>
              Public site: <strong>ai-northstar.com</strong>
            </p>
          </div>

          <div className="docs-grid">
            <article className="feature-card">
              <p className="eyebrow">Starter Kit</p>
              <h3>What the Starter Kit includes</h3>
              <ul className="feature-list">
                <li>Account creation, billing management, and a guided onboarding path</li>
                <li>Local agent download and setup guidance for your machine</li>
                <li>AI copilot access for prompts, summaries, and step-by-step guidance</li>
              </ul>
              <a className="button button-secondary" href="/site/subscribe">Choose Starter Kit</a>
            </article>

            <article className="feature-card">
              <p className="eyebrow">HQ + Local Agent</p>
              <h3>Built for advanced teams</h3>
              <ul className="feature-list">
                <li>Admin-ready workflows for shared knowledge and larger operating contexts</li>
                <li>Richer operational views that build on the local-agent foundation</li>
                <li>Early access to the HQ experience for internal evaluation and rollout planning</li>
              </ul>
              <a className="button button-secondary" href="/app/hq">Preview HQ experience</a>
            </article>
          </div>

          <div className="docs-grid docs-grid-wide">
            <article className="feature-card">
              <p className="eyebrow">How the experience works</p>
              <h3>Local agent + AI copilot</h3>
              <p className="muted-copy">
                The local agent handles tasks on the user&apos;s machine while the AI copilot explains the workflow, organizes context, and helps the user decide what to do next. Together they create a practical loop: the copilot suggests the action, and the agent carries it out close to the data.
              </p>
              <div className="capability-list">
                <div>
                  <strong>Local agent</strong>
                  <p>Runs on your computer, keeps local workflows close to the source, and supports installation and setup.</p>
                </div>
                <div>
                  <strong>AI copilot</strong>
                  <p>Provides the product-facing guidance, planning, and context for the workflow experience.</p>
                </div>
                <div>
                  <strong>Pairing model</strong>
                  <p>The copilot suggests the next action and the local agent carries it out where it matters most.</p>
                </div>
              </div>
            </article>

            <article className="feature-card">
              <p className="eyebrow">Site capabilities</p>
              <h3>What you can do on the site</h3>
              <ul className="feature-list">
                <li>Create an account and start the guided onboarding experience</li>
                <li>Choose a plan, manage billing, and keep your subscription current</li>
                <li>Download and set up the local agent for your preferred environment</li>
                <li>Unsubscribe or unregister cleanly when you no longer need the service</li>
              </ul>
            </article>
          </div>

          <article className="feature-card">
            <p className="eyebrow">Polyglot AI library</p>
            <h3>Northstar AI is available across languages</h3>
            <p className="muted-copy">
              Once your subscription is active, you are licensed to use the Northstar AI polyglot library from C#, Rust, Java, Python, and Go. The shared ai/memory package and KnowledgeBase APIs let teams build memory, retrieval, and knowledge workflows for end-user applications and for microservices that need a consistent integration surface, and the bundled httpserver and daemon enforce that subscription-bound access for service-side use.
            </p>
            <ul className="feature-list">
              <li>Your active plan unlocks the polyglot library in the supported runtimes.</li>
              <li>Use the same KnowledgeBase concepts from Go, Python, Java, C#, or Rust.</li>
              <li>Integrate directly from end-user applications or access the same capabilities through microservices endpoints.</li>
              <li>Keep your AI experience consistent across desktop, server, and cloud deployments.</li>
            </ul>
          </article>

          <div className="docs-journey">
            <div className="journey-step">
              <span className="journey-number">1</span>
              <div>
                <h3>Register</h3>
                <p>Create your account and start the guided onboarding flow in just a few clicks.</p>
              </div>
            </div>
            <div className="journey-step">
              <span className="journey-number">2</span>
              <div>
                <h3>Choose a plan</h3>
                <p>Pick the Starter Kit for a simple entry point or preview the HQ path for advanced workflows.</p>
              </div>
            </div>
            <div className="journey-step">
              <span className="journey-number">3</span>
              <div>
                <h3>Install and pair</h3>
                <p>Download the local agent and let the AI copilot guide the next actions for your workflow.</p>
              </div>
            </div>
          </div>

          <div className="docs-comparison">
            <article className="feature-card comparison-card">
              <p className="eyebrow">Starter Kit</p>
              <h3>Best for getting started</h3>
              <p className="muted-copy">A straightforward path to account setup, local-agent installation, and guided product use.</p>
            </article>
            <article className="feature-card comparison-card">
              <p className="eyebrow">HQ + Local Agent</p>
              <h3>Best for advanced teams</h3>
              <p className="muted-copy">A preview path for richer admin and team workflows that build on the same local-agent foundation.</p>
            </article>
          </div>
        </section>
      </main>
    );
  }

  return (
    <main className="app-shell">
      <section className="hero-card">
        <div className="hero-grid">
          <div>
            <p className="eyebrow">Northstar</p>
            <h1 className="hero-title">Turn scattered knowledge into guided AI workflows</h1>
            <p className="hero-copy">
              Northstar gives teams a local-first way to organize Stores, Spaces, and Knowledge Bases, then pair them with an AI copilot that can guide work, summarize context, and help move projects forward without losing control of the data.
            </p>
            <div className="hero-actions">
              <a className="button button-primary" href="/site/subscribe">Create account</a>
              <a className="button button-secondary" href="/site/docs">View product docs</a>
            </div>
          </div>

          <div className="hero-side">
            <div className="plan-card">
              <p className="eyebrow">Starter Kit</p>
              <h3>Guided onboarding for local-first AI work</h3>
              <div className="price">$5/month</div>
              <p>Start with a simple setup path that covers account creation, subscription management, local-agent installation, and the first AI-enabled workflows.</p>
              <ul className="feature-list">
                <li>Local agent access for hands-on workflows on your machine</li>
                <li>AI copilot support for prompts, summaries, and next-step guidance</li>
                <li>Pre-built Knowledge Base patterns for coding and review tasks</li>
              </ul>
            </div>
          </div>
        </div>
      </section>

      <section className="hero-card secondary-card">
        <div className="stack-grid">
          <div className="form-panel">
            <h2>{isStarterActive ? 'Your Starter Kit is active' : isStarterPending ? 'Your Starter Kit checkout is pending' : 'Start your Starter Kit'}</h2>
            <p className="muted-copy">
              {isStarterActive
                ? `Welcome back, ${account.name || 'there'}. Your Starter Kit is active and ready for local agent setup and guided AI workflows.`
                : isStarterPending
                  ? `Welcome back, ${account.name || 'there'}. Your Starter Kit checkout is pending payment confirmation.`
                  : 'Create your account to start with guided onboarding, a local agent download, and a practical first workflow.'}
            </p>
            <form className="form-stack" onSubmit={handleStart}>
              <label>
                Name
                <input value={name} onChange={(event) => setName(event.target.value)} placeholder="Alex Rivera" />
              </label>
              <label>
                Email
                <input type="email" value={email} onChange={(event) => setEmail(event.target.value)} placeholder="alex@example.com" />
              </label>
              <button type="submit" className="button button-primary" disabled={isSubmitting}>
                {isStarterActive ? 'Update account' : isSubmitting ? 'Working…' : 'Start Starter Kit'}
              </button>
            </form>
            {statusMessage ? <p className="muted-copy">{statusMessage}</p> : null}
          </div>

          <div className="summary-panel">
            <h3>Account summary</h3>
            <div className="summary-list">
              <div>
                <span className="summary-label">Status</span>
                <strong>{isStarterActive ? 'Starter Kit active' : isStarterPending ? 'Checkout pending' : 'Not subscribed'}</strong>
              </div>
              <div>
                <span className="summary-label">Plan</span>
                <strong>{isStarterActive ? 'Starter Kit' : isStarterPending ? 'Starter Kit (pending)' : 'No active plan'}</strong>
              </div>
              <div>
                <span className="summary-label">Included</span>
                <strong>AI copilot, Stores, Spaces, Knowledge Bases</strong>
              </div>
            </div>

            <div className="status-card">
              <div className="status-pill">{account.isAuthenticated ? 'Signed in' : 'Not signed in'}</div>
              <div className="status-details">
                <strong>{account.name || 'No account linked yet'}</strong>
                <span>{account.email || 'Create an account to start the guided flow'}</span>
              </div>
            </div>

            {account.payments.length > 0 ? (
              <div className="preview-box">
                <h4>Most recent payment</h4>
                <p>
                  {account.payments[0].status === 'succeeded'
                    ? `Paid ${account.payments[0].amount} ${account.payments[0].currency || 'USD'} for ${account.subscriptions[0]?.planCode || 'your plan'}.`
                    : `Payment status: ${account.payments[0].status}.`}
                </p>
              </div>
            ) : null}

            <div className="action-row">
              <button
                type="button"
                className="button button-secondary"
                onClick={() => {
                  void (async () => {
                    try {
                      const refreshed = await loadAccountFromServer();
                      setAccount(refreshed);
                      if (refreshed.isAuthenticated) {
                        setName(refreshed.name || name);
                        setEmail(refreshed.email || email);
                        setShowInstallStep(refreshed.billingStatus === 'active' && refreshed.plan === 'starter');
                        setCheckoutComplete(refreshed.billingStatus === 'active' && refreshed.plan === 'starter');
                        setStatusMessage('Account status refreshed.');
                      }
                    } catch {
                      setStatusMessage('Unable to refresh account status right now.');
                    }
                  })();
                }}
                disabled={isSubmitting}
              >
                Refresh status
              </button>
              <button type="button" className="button button-secondary" onClick={handleLogin} disabled={isSubmitting || !name.trim() || !email.trim()}>
                Log in
              </button>
              <button type="button" className="button button-secondary" onClick={handleLogout} disabled={isSubmitting}>
                Log out
              </button>
              <a className="button button-secondary" href="/site/subscribe">Go to subscription flow</a>
              <button type="button" className="button button-secondary" onClick={handleUnsubscribe} disabled={isSubmitting}>
                Unsubscribe
              </button>
              <button type="button" className="button button-secondary danger" onClick={handleUnregister} disabled={isSubmitting}>
                Unregister
              </button>
            </div>

            {shouldShowInstallPanel ? (
              <div className="preview-box install-box">
                <h4>Next step: install the local agent</h4>
                <p>Your subscription is active, so the local agent is ready to install. Download it from the official SOP releases page and pair it with the AI copilot to launch the host-side workflow.</p>
                <button type="button" className="button button-secondary" onClick={handleDownload} disabled={isSubmitting}>
                  {isSubmitting ? 'Preparing…' : 'Download local agent'}
                </button>
              </div>
            ) : null}
          </div>
        </div>
      </section>
    </main>
  );
}
