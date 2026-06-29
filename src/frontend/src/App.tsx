import { useEffect, useState, type FormEvent } from 'react';

type Plan = 'starter' | 'hq' | 'none';
type ViewMode = 'home' | 'subscribe' | 'docs';

type AccountState = {
  name: string;
  email: string;
  plan: Plan;
  trialActive: boolean;
  isAuthenticated: boolean;
};

const emptyState: AccountState = {
  name: '',
  email: '',
  plan: 'none',
  trialActive: false,
  isAuthenticated: false,
};

const DRAFT_STORAGE_KEY = 'sop-cloud-site-draft';

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
  const response = await fetch('/api/auth/me', { credentials: 'include' });
  if (!response.ok) {
    return emptyState;
  }
  const payload = await response.json();
  const user = payload?.user ?? {};
  const plan = normalizePlan(user.subscription_plan);
  return {
    name: user.username ?? '',
    email: user.email ?? '',
    plan,
    trialActive: plan === 'starter',
    isAuthenticated: true,
  };
}

async function ensureAuthenticatedAccount(name: string, email: string): Promise<AccountState> {
  const username = buildUsername(name, email);
  const password = buildPassword(email);

  const loginResponse = await fetch('/api/auth/login', {
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

    const followUpLoginResponse = await fetch('/api/auth/login', {
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

async function createCheckoutSession(plan: Plan): Promise<{ url: string }> {
  const payload = await fetchJSON('/api/auth/checkout', {
    method: 'POST',
    body: JSON.stringify({ plan }),
  });
  return { url: payload?.checkout?.url || '' };
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
    setName(draft.name);
    setEmail(draft.email);
    void (async () => {
      try {
        const nextAccount = await loadAccountFromServer();
        setAccount(nextAccount);
        if (nextAccount.isAuthenticated) {
          setName(nextAccount.name || draft.name);
          setEmail(nextAccount.email || draft.email);
          setShowInstallStep(nextAccount.plan === 'starter');
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
      setAccount({
        ...nextAccount,
        name: name.trim(),
        email: email.trim(),
        plan: 'starter',
        trialActive: true,
        isAuthenticated: true,
      });
      setShowInstallStep(true);
      setView('home');
      setStatusMessage('Your account is now connected to the backend and your starter plan is active.');
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
      const checkout = await createCheckoutSession('starter');
      if (checkout.url) {
        window.location.assign(checkout.url);
        return;
      }
      const synced = await updateSubscriptionPlan('starter');
      setAccount({
        ...nextAccount,
        name: nextAccount.name || account.name || name.trim(),
        email: nextAccount.email || account.email || email.trim(),
        plan: synced.plan,
        trialActive: true,
        isAuthenticated: true,
      });
      setShowInstallStep(true);
      setCheckoutComplete(true);
      setStatusMessage('Subscription confirmed. Your starter plan is now active.');
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
        trialActive: false,
        isAuthenticated: true,
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

  const isStarterActive = account.plan === 'starter';
  const isPreviewVisible = account.plan === 'starter' || account.plan === 'hq';

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
              <h2>Start your Starter Kit subscription</h2>
              <p className="muted-copy">
                Your card is handled securely and we only need the minimum information required to start the monthly subscription. An active subscription unlocks licensed use of the SOP AI polyglot library in the supported runtimes, including the bundled service-side experiences that enforce subscription-bound security tokens.
              </p>

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

              {checkoutComplete ? (
                <div className="success-banner">
                  <h4>Subscription confirmed</h4>
                  <p>Your Starter Kit is active, your access to the SOP AI polyglot library is now enabled, and the local-agent download step is ready.</p>
                </div>
              ) : (
                <div className="preview-box">
                  <h4>What happens next</h4>
                  <p>After checkout, we guide you to install the local agent and continue with the onboarding flow.</p>
                </div>
              )}
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
            <h1 className="hero-title">See what the SOP Cloud experience includes, from the local agent to the AI copilot.</h1>
            <p className="hero-copy">
              The new site is designed for self-service onboarding. It lets users register, choose a plan, manage their subscription, and understand how the local agent and the AI copilot work together. An active subscription unlocks licensed use of the SOP AI polyglot library across supported runtimes, and the bundled httpserver and daemon enforce that subscription-bound access for service-side use.
            </p>
          </div>

          <div className="docs-grid">
            <article className="feature-card">
              <p className="eyebrow">Starter Kit</p>
              <h3>What is included</h3>
              <ul className="feature-list">
                <li>Local agent download and setup guidance for your machine</li>
                <li>AI copilot access for prompts, summaries, and guided workflows</li>
                <li>Simple account management for registration, subscription changes, and unregistering</li>
              </ul>
              <a className="button button-secondary" href="/site/subscribe">Choose Starter Kit</a>
            </article>

            <article className="feature-card">
              <p className="eyebrow">HQ + Local Agent</p>
              <h3>Planned for advanced teams</h3>
              <ul className="feature-list">
                <li>Broader team-focused workflows for admins and power users</li>
                <li>Richer operational views that build on the local-agent foundation</li>
                <li>Preview access for internal testing before wider rollout</li>
              </ul>
              <a className="button button-secondary" href="/app/hq">Preview HQ experience</a>
            </article>
          </div>

          <div className="docs-grid docs-grid-wide">
            <article className="feature-card">
              <p className="eyebrow">How the experience works</p>
              <h3>Local agent + AI copilot</h3>
              <p className="muted-copy">
                The local agent handles tasks on the user&apos;s machine, while the AI copilot orchestrates the workflow, explains the next step, and helps users stay in control. They work best together because the copilot can suggest an action and the local agent can execute it close to the data.
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
              <h3>User actions available on the site</h3>
              <ul className="feature-list">
                <li>Register a new account and create a starter profile</li>
                <li>Sign up for a subscription plan and move into the guided onboarding flow</li>
                <li>Unsubscribe from the active plan when you no longer need it</li>
                <li>Unregister your account and remove the site-side identity cleanly</li>
              </ul>
            </article>
          </div>

          <article className="feature-card">
            <p className="eyebrow">Polyglot AI library</p>
            <h3>SOP AI is available across languages</h3>
            <p className="muted-copy">
              Once your subscription is active, you are licensed to use the SOP AI polyglot library from C#, Rust, Java, Python, and Go. The shared ai/memory package and KnowledgeBase APIs let teams build memory, retrieval, and knowledge workflows for end-user applications and for microservices that need a consistent integration surface, and the bundled httpserver and daemon enforce that subscription-bound access for service-side use.
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
            <p className="eyebrow">SOP Cloud</p>
            <h1 className="hero-title">Launch the local agent, manage your account, and grow into the next experience.</h1>
            <p className="hero-copy">
              This product surface is designed for self-serve onboarding and subscription management. It is intentionally separate from the Data Manager so each experience stays focused on its own workflow.
            </p>
            <div className="hero-actions">
              <a className="button button-primary" href="/site/docs">View product docs</a>
              <a className="button button-secondary" href="/app">Open Data Manager</a>
            </div>
          </div>

          <div className="hero-side">
            <div className="plan-card">
              <p className="eyebrow">Starter Kit</p>
              <h3>Local Agent</h3>
              <div className="price">$5/mo</div>
              <p>Download the local agent and begin working from your own machine.</p>
              <a className="button button-secondary" href="/site/docs">See plan features</a>
              <span className="badge">Trial access included</span>
            </div>
            <div className="plan-card">
              <p className="eyebrow">Next plan</p>
              <h3>HQ + Local Agent</h3>
              <p>Coming soon as a richer experience for admins and advanced teams.</p>
              <a className="button button-secondary" href="/site/docs">See plan features</a>
              <span className="badge">Preview available</span>
            </div>
          </div>
        </div>

        <div className="section-row">
          <div className="info-card">
            <h4>Fast onboarding</h4>
            <p>Create an account, pick a plan, and start using the local agent with a guided flow.</p>
          </div>
          <div className="info-card">
            <h4>Account control</h4>
            <p>Return later to view plan status, manage subscriptions, or unregister cleanly.</p>
          </div>
          <div className="info-card">
            <h4>Separate product surface</h4>
            <p>The Data Manager remains lightweight and operational while this site stays product-first.</p>
          </div>
        </div>
      </section>

      <section className="hero-card secondary-card">
        <div className="pricing-strip">
          <div className="pricing-card highlighted">
            <p className="eyebrow">Starter Kit</p>
            <h3>Try the local agent</h3>
            <div className="price">$5/mo after the first month</div>
            <p>Simple onboarding, local-agent install, and a clear path to the full SOP experience.</p>
          </div>
          <div className="pricing-card">
            <p className="eyebrow">HQ + Local Agent</p>
            <h3>For teams and admins</h3>
            <div className="price">Coming soon</div>
            <p>Preview the richer experience before it becomes generally available.</p>
          </div>
        </div>

        <div className="stack-grid">
          <div className="form-panel">
            <h2>{isStarterActive ? 'Your Starter Kit is active' : 'Start your Starter Kit'}</h2>
            <p className="muted-copy">
              {isStarterActive
                ? `Welcome back, ${account.name || 'there'}. Your starter plan is active and ready for local-agent setup.`
                : 'Register your account and begin the guided starter experience.'}
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
                <strong>{isStarterActive ? 'Starter Kit active' : 'Not subscribed'}</strong>
              </div>
              <div>
                <span className="summary-label">Plan</span>
                <strong>{isStarterActive ? 'Starter Kit' : 'No active plan'}</strong>
              </div>
              <div>
                <span className="summary-label">Trial</span>
                <strong>{isStarterActive ? 'Active for the first month' : 'Not started'}</strong>
              </div>
            </div>

            <div className="action-row">
              <a className="button button-secondary" href="/site/subscribe">Go to subscription flow</a>
              <button type="button" className="button button-secondary" onClick={handleUnsubscribe} disabled={isSubmitting}>
                Unsubscribe
              </button>
              <button type="button" className="button button-secondary danger" onClick={handleUnregister} disabled={isSubmitting}>
                Unregister
              </button>
            </div>

            {showInstallStep || isStarterActive ? (
              <div className="preview-box install-box">
                <h4>Next step: install the local agent</h4>
                <p>Download the release binary from the SOP GitHub releases page and continue the setup on your machine.</p>
                <a className="button button-secondary" href="https://github.com/sharedcode/sop/releases" target="_blank" rel="noreferrer">
                  Open releases page
                </a>
              </div>
            ) : null}

            {isPreviewVisible ? (
              <div className="preview-box">
                <h4>Next: HQ + Local Agent</h4>
                <p>Admins and advanced teams can preview the richer experience while regular users stay on the starter path.</p>
                <a className="button button-secondary" href="/app/hq">Preview HQ experience</a>
              </div>
            ) : null}
          </div>
        </div>
      </section>
    </main>
  );
}
