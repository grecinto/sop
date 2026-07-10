import { useEffect, useState, useRef } from 'react';

type Plan = 'starter' | 'hq' | 'none';
type ViewMode = 'home' | 'subscribe' | 'docs' | 'success' | 'cancel';

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
  role: string;
  plan: Plan;
  billingStatus: BillingStatus;
  trialActive: boolean;
  isAuthenticated: boolean;
  isSystemAdmin: boolean;
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
  role: '',
  plan: 'none',
  billingStatus: 'inactive',
  trialActive: false,
  isAuthenticated: false,
  isSystemAdmin: false,
  checkoutUrl: '',
  lastWebhookEvent: '',
  failureReason: '',
  lastWebhookAt: '',
  payments: [],
  subscriptions: [],
};

const DRAFT_STORAGE_KEY = 'sop-cloud-site-draft';
const ACCOUNT_STATE_STORAGE_KEY = 'sop-cloud-site-account-state';
const PROVISIONING_DONE_STORAGE_KEY = 'sop_provisioning_done';
const SIGNED_OUT_STORAGE_KEY = 'sop-site-signed-out';
const PENDING_CHECKOUT_SESSION_STORAGE_KEY = 'sop-cloud-site-pending-checkout-session';

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

function loadProvisioningDoneFlag(): boolean {
  if (typeof window === 'undefined') {
    return false;
  }
  try {
    return window.localStorage.getItem(PROVISIONING_DONE_STORAGE_KEY) === '1';
  } catch {
    return false;
  }
}

function clearPersistedAccountState() {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.removeItem(ACCOUNT_STATE_STORAGE_KEY);
  window.localStorage.removeItem(DRAFT_STORAGE_KEY);
  window.sessionStorage.removeItem(ACCOUNT_STATE_STORAGE_KEY);
}

function loadPendingCheckoutSessionID(): string {
  if (typeof window === 'undefined') {
    return '';
  }
  try {
    return window.localStorage.getItem(PENDING_CHECKOUT_SESSION_STORAGE_KEY) ?? '';
  } catch {
    return '';
  }
}

function persistPendingCheckoutSessionID(sessionID: string) {
  if (typeof window === 'undefined') {
    return;
  }
  if (!sessionID) {
    window.localStorage.removeItem(PENDING_CHECKOUT_SESSION_STORAGE_KEY);
    return;
  }
  window.localStorage.setItem(PENDING_CHECKOUT_SESSION_STORAGE_KEY, sessionID);
}

function clearPendingCheckoutSessionID() {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.removeItem(PENDING_CHECKOUT_SESSION_STORAGE_KEY);
}

function markSignedOut() {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.setItem(SIGNED_OUT_STORAGE_KEY, '1');
  window.sessionStorage.setItem(SIGNED_OUT_STORAGE_KEY, '1');
}

function clearSignedOutFlag() {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.removeItem(SIGNED_OUT_STORAGE_KEY);
  window.sessionStorage.removeItem(SIGNED_OUT_STORAGE_KEY);
}

function consumeLogoutPendingFlag(): boolean {
  if (typeof window === 'undefined') {
    return false;
  }
  const pending = window.sessionStorage.getItem('sop-site-logout-pending') === '1' || window.localStorage.getItem('sop-site-logout-pending') === '1';
  const signedOut = window.sessionStorage.getItem(SIGNED_OUT_STORAGE_KEY) === '1' || window.localStorage.getItem(SIGNED_OUT_STORAGE_KEY) === '1';
  if (pending || signedOut) {
    window.sessionStorage.removeItem('sop-site-logout-pending');
    window.localStorage.removeItem('sop-site-logout-pending');
  }
  return pending || signedOut;
}

function persistAccountState(account: AccountState) {
  if (typeof window === 'undefined') {
    return;
  }
  if (!account.isAuthenticated) {
    clearPersistedAccountState();
    return;
  }
  window.localStorage.setItem(
    ACCOUNT_STATE_STORAGE_KEY,
    JSON.stringify({
      name: account.name,
      email: account.email,
      role: account.role,
      plan: account.plan,
      billingStatus: account.billingStatus,
      isAuthenticated: account.isAuthenticated,
      isSystemAdmin: account.isSystemAdmin,
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
  if (pathname.startsWith('/site/subscribe/success')) {
    return 'success';
  }
  if (pathname.startsWith('/site/subscribe/cancel')) {
    return 'cancel';
  }
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

function normalizeRole(value: string | undefined): string {
  const role = (value ?? '').trim();
  if (!role) {
    return '';
  }
  const normalized = role.toLowerCase();
  if (normalized === 'systemadmin' || normalized === 'system admin') {
    return 'SystemAdmin';
  }
  if (normalized === 'admin') {
    return 'Admin';
  }
  if (normalized === 'user') {
    return 'User';
  }
  return role;
}

function isSystemAdminRole(role: string | undefined): boolean {
  const normalized = (role ?? '').trim().toLowerCase();
  return normalized === 'systemadmin' || normalized === 'system admin';
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 24) || 'sop-user';
}

function createTemporarySuffix(): string {
  const randomSource = typeof globalThis !== 'undefined' && typeof globalThis.crypto !== 'undefined' && typeof globalThis.crypto.randomUUID === 'function'
    ? globalThis.crypto
    : null;
  if (randomSource) {
    return randomSource.randomUUID().replace(/-/g, '').slice(0, 12);
  }
  return `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
}

function buildUsername(name: string, email: string): string {
  const trimmedName = (name || '').trim();
  const trimmedEmail = (email || '').trim();
  if (trimmedName) {
    if (trimmedName.includes('@')) {
      return trimmedName;
    }
    if (trimmedName === 'root' || trimmedName.toLowerCase() === 'sa' || trimmedName.toLowerCase() === 'systemadmin' || trimmedName.toLowerCase() === 'system admin') {
      return trimmedName;
    }
    if (!trimmedEmail) {
      return trimmedName;
    }
  }
  const base = slugify(trimmedName || trimmedEmail || `guest-${createTemporarySuffix()}`);
  const suffix = trimmedEmail.toLowerCase().replace(/[^a-z0-9]+/g, '');
  return suffix ? `${base}-${suffix.slice(0, 8)}` : `${base}-${createTemporarySuffix().slice(0, 8)}`;
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

async function pingPendingCheckout(): Promise<void> {
  try {
    console.info('[site-subscribe] pinging pending checkout recovery endpoint');
    const response = await fetch('/api/auth/ping', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
    });
    console.info('[site-subscribe] pending checkout ping response', { ok: response.ok, status: response.status });
  } catch (error) {
    console.warn('[site-subscribe] pending checkout ping failed', error);
    // Ignore ping failures; the status poller will keep retrying and the server can still recover.
  }
}

async function recoverPendingCheckout(sessionID: string): Promise<AccountState | null> {
  if (!sessionID) {
    return null;
  }
  try {
    const response = await fetch('/api/auth/pending-checkout/recover', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ checkout_session_id: sessionID }),
    });
    if (!response.ok) {
      return null;
    }
    const payload = await response.json();
    const user = payload?.user ?? {};
    const plan = normalizePlan(user.subscription_plan);
    const billingStatus = normalizeBillingStatus(user.billing_status);
    const billing = user.billing ?? {};
    const role = normalizeRole(user.role);
    const canAccessDataManager = Boolean(user.can_access_data_manager) || isSystemAdminRole(role);
    return {
      name: user.username ?? '',
      email: user.email ?? '',
      role,
      plan,
      billingStatus,
      trialActive: billingStatus === 'active' && plan === 'starter',
      isAuthenticated: true,
      isSystemAdmin: canAccessDataManager,
      checkoutUrl: billing.checkout_url ?? '',
      lastWebhookEvent: billing.last_webhook_event ?? '',
      failureReason: billing.failure_reason ?? '',
      lastWebhookAt: billing.last_webhook_at ?? '',
      payments: [],
      subscriptions: [],
    };
  } catch {
    return null;
  }
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
  const role = normalizeRole(user.role);
  const canAccessDataManager = Boolean(user.can_access_data_manager) || isSystemAdminRole(role);
  return {
    name: user.username ?? '',
    email: user.email ?? '',
    role,
    plan,
    billingStatus,
    trialActive: billingStatus === 'active' && plan === 'starter',
    isAuthenticated: true,
    isSystemAdmin: canAccessDataManager,
    checkoutUrl: billing.checkout_url ?? '',
    lastWebhookEvent: billing.last_webhook_event ?? '',
    failureReason: billing.failure_reason ?? '',
    lastWebhookAt: billing.last_webhook_at ?? '',
    payments: Array.isArray(user.payments) ? user.payments : [],
    subscriptions: Array.isArray(user.subscriptions) ? user.subscriptions : [],
  };
}

function mergeAccountWithFallback(nextAccount: AccountState, fallback: AccountState, draftName: string, draftEmail: string): AccountState {
  return {
    ...nextAccount,
    name: nextAccount.name || fallback.name || draftName,
    email: nextAccount.email || fallback.email || draftEmail,
    role: nextAccount.role || fallback.role || '',
    isSystemAdmin: nextAccount.isSystemAdmin || isSystemAdminRole(nextAccount.role || fallback.role),
    trialActive: nextAccount.billingStatus === 'active' && nextAccount.plan === 'starter',
  };
}

async function getHumanProofChallenge(): Promise<string> {
  const response = await fetch('/api/auth/human-proof/challenge', { credentials: 'include' });
  if (!response.ok) {
    throw new Error('Unable to start the verification step.');
  }
  const payload = await response.json();
  return typeof payload?.challenge === 'string' ? payload.challenge : '';
}

async function ensureAuthenticatedAccount(name: string, email: string, password: string, humanProofToken: string): Promise<AccountState> {
  const username = buildUsername(name.trim() || email.trim(), email.trim() || name.trim());
  const resolvedEmail = email.trim() || `${username}@sop.local`;

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
        email: resolvedEmail,
        role: 'user',
        subscription_plan: 'starter',
        request_origin: 'human',
        human_proof: humanProofToken,
      }),
    });

    if (!registerResponse.ok && registerResponse.status !== 409) {
      const registerPayload = await registerResponse.json().catch(() => null);
      const registerMessage = registerPayload?.message || registerPayload?.error || 'Unable to create your account.';
      throw new Error(registerMessage);
    }

    const followUpLoginResponse = await fetch('/api/auth/login', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    if (!followUpLoginResponse.ok) {
      const followUpPayload = await followUpLoginResponse.json().catch(() => null);
      const followUpMessage = followUpPayload?.message || followUpPayload?.error || 'Unable to sign in after account creation.';
      throw new Error(followUpMessage);
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

async function createCheckoutSession(plan: Plan, humanProofToken: string, name: string, email: string): Promise<{ id: string; url: string; status: string }> {
  const payload = await fetchJSON('/api/auth/checkout', {
    method: 'POST',
    body: JSON.stringify({ plan, name, email, request_origin: 'human', human_proof: humanProofToken }),
  });
  return {
    id: payload?.checkout?.id || '',
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
  const response = await fetch('/api/auth/login', {
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

async function changePassword(currentPassword: string, newPassword: string): Promise<AccountState> {
  const payload = await fetchJSON('/api/auth/password', {
    method: 'POST',
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  });
  if (!payload?.user) {
    throw new Error('Unable to update your password right now.');
  }
  return loadAccountFromServer();
}

async function logoutFromServer(): Promise<void> {
  await fetch('/api/auth/logout', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
  });
}

export default function App() {
  const [account, setAccount] = useState<AccountState>(emptyState);
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [newPasswordConfirm, setNewPasswordConfirm] = useState('');
  const [showInstallStep, setShowInstallStep] = useState(false);
  const [view, setView] = useState<ViewMode>(() => resolveView(typeof window === 'undefined' ? '/' : window.location.pathname));
  const [checkoutComplete, setCheckoutComplete] = useState(false);
  const [statusMessage, setStatusMessage] = useState('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isRefreshingBilling, setIsRefreshingBilling] = useState(false);
  const [provisioningDone, setProvisioningDone] = useState(false);
  const isRefreshingRef = useRef(false);

  useEffect(() => {
    const pendingLogout = consumeLogoutPendingFlag();
    if (pendingLogout) {
      console.info('auth bootstrap skipped after logout');
      setAccount(emptyState);
      clearPersistedAccountState();
      markSignedOut();
      setName('');
      setEmail('');
      setPassword('');
      setConfirmPassword('');
      setCurrentPassword('');
      setNewPassword('');
      setNewPasswordConfirm('');
      setShowInstallStep(false);
      setCheckoutComplete(false);
      return;
    }

    const draft = loadDraftState();
    const pendingCheckoutSessionID = loadPendingCheckoutSessionID();
    setProvisioningDone(loadProvisioningDoneFlag());
    setName(draft.name);
    setEmail(draft.email);
    void (async () => {
      try {
        console.info('auth bootstrap probing server status');
        let resolvedAccount = await loadAccountFromServer();
        if (!resolvedAccount.isAuthenticated && pendingCheckoutSessionID) {
          const recoveredAccount = await recoverPendingCheckout(pendingCheckoutSessionID);
          if (recoveredAccount?.isAuthenticated) {
            resolvedAccount = recoveredAccount;
          }
        }
        const nextAccount: AccountState = {
          ...resolvedAccount,
          name: resolvedAccount.name || draft.name,
          email: resolvedAccount.email || draft.email,
          role: resolvedAccount.role || '',
          isSystemAdmin: resolvedAccount.isSystemAdmin || isSystemAdminRole(resolvedAccount.role),
          trialActive: resolvedAccount.billingStatus === 'active' && resolvedAccount.plan === 'starter',
        };
        setAccount(nextAccount);
        if (nextAccount.isAuthenticated) {
          console.info('auth bootstrap restored authenticated account', { role: nextAccount.role, name: nextAccount.name });
          setName(nextAccount.name || draft.name);
          setEmail(nextAccount.email || draft.email);
          setShowInstallStep(nextAccount.billingStatus === 'active' && nextAccount.plan === 'starter');
          setCheckoutComplete(nextAccount.billingStatus === 'active' && nextAccount.plan === 'starter');
          if (nextAccount.billingStatus === 'pending') {
            void pingPendingCheckout();
          } else {
            clearPendingCheckoutSessionID();
          }
          persistAccountState(nextAccount);
        } else {
          console.info('auth bootstrap found no authenticated account');
          clearPersistedAccountState();
          setShowInstallStep(false);
          setCheckoutComplete(false);
        }
      } catch (error) {
        console.warn('auth bootstrap failed', error);
        setAccount(emptyState);
        clearPersistedAccountState();
        setShowInstallStep(false);
        setCheckoutComplete(false);
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

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }

    if (view !== 'success' && view !== 'subscribe') {
      return;
    }

    console.info('[site-subscribe] starting 30s auto-refresh timer');
    const timer = window.setInterval(() => {
      console.info('[site-subscribe] 30s elapsed - calling handleRecheckBillingStatus');
      if (!isRefreshingRef.current) {
        void handleRecheckBillingStatus();
      }
    }, 30000);

    return () => {
      console.info('[site-subscribe] stopping auto-refresh timer');
      window.clearInterval(timer);
    };
  }, [view]);

  const handleSubscription = async () => {
    setIsSubmitting(true);
    setStatusMessage('');
    try {
      const serverAccount = await loadAccountFromServer();
      const resolvedAuthAccount = mergeAccountWithFallback(serverAccount, account, name, email);
      if (!resolvedAuthAccount.isAuthenticated) {
        if (!password.trim()) {
          throw new Error('Please choose a password to continue.');
        }
        if (password.trim() !== confirmPassword.trim()) {
          throw new Error('Your chosen passwords do not match.');
        }
      }
      const token = await getHumanProofChallenge();
      const nextAccount = resolvedAuthAccount.isAuthenticated
        ? resolvedAuthAccount
        : await ensureAuthenticatedAccount(account.name || name.trim(), account.email || email.trim(), password.trim(), token);
      if (!nextAccount.isAuthenticated) {
        throw new Error('Please sign in or create an account before continuing.');
      }
      const effectiveName = nextAccount.name || account.name || name.trim();
      const effectiveEmail = nextAccount.email || account.email || email.trim();
      const checkout = await createCheckoutSession('starter', token, effectiveName, effectiveEmail);
      if (checkout.url) {
        persistPendingCheckoutSessionID(checkout.id);
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
      if (synced.billingStatus !== 'pending') {
        clearPendingCheckoutSessionID();
      }
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

  const handlePasswordChange = async () => {
    setIsSubmitting(true);
    setStatusMessage('');
    try {
      if (!currentPassword.trim()) {
        throw new Error('Please enter your current password.');
      }
      if (!newPassword.trim()) {
        throw new Error('Please choose a new password.');
      }
      if (newPassword.trim() !== newPasswordConfirm.trim()) {
        throw new Error('Your new passwords do not match.');
      }
      const synced = await changePassword(currentPassword.trim(), newPassword.trim());
      setAccount(synced);
      setCurrentPassword('');
      setNewPassword('');
      setNewPasswordConfirm('');
      setStatusMessage('Your password has been updated.');
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to update your password right now.');
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleUnsubscribe = async () => {
    setIsSubmitting(true);
    setStatusMessage('');
    try {
      const synced = await updateSubscriptionPlan('none');
      clearPendingCheckoutSessionID();
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
      setStatusMessage('Your subscription has been canceled. Your site account remains available for sign-in.');
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
      clearPersistedAccountState();
      clearPendingCheckoutSessionID();
      if (typeof window !== 'undefined') {
        window.localStorage.removeItem(DRAFT_STORAGE_KEY);
      }
      setName('');
      setEmail('');
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
    const existingIdentity = (name.trim() || account.name || email.trim() || account.email || '').trim();
    const resolvedUsername = existingIdentity || (typeof window !== 'undefined' ? window.prompt('Enter your username to sign in.') ?? '' : '');
    if (!resolvedUsername) {
      setStatusMessage('Please enter your username before signing in.');
      return;
    }
    const enteredPassword = password.trim() || (typeof window !== 'undefined' ? window.prompt('Enter your password to sign in.') ?? '' : '');
    if (!enteredPassword.trim()) {
      setStatusMessage('Please enter your password to sign in.');
      return;
    }
    setPassword(enteredPassword.trim());
    setIsSubmitting(true);
    setStatusMessage('');
    try {
      const synced = await loginWithCredentials(resolvedUsername, enteredPassword.trim());
      const resolvedAccount = {
        ...synced,
        name: synced.name || resolvedUsername,
        email: synced.email || email.trim() || account.email || '',
        role: synced.role || '',
        isSystemAdmin: synced.isSystemAdmin || isSystemAdminRole(synced.role),
        trialActive: synced.billingStatus === 'active' && synced.plan === 'starter',
      };
      setName(resolvedUsername);
      setEmail(resolvedAccount.email || '');
      clearSignedOutFlag();
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
    console.info('logout requested');
    if (typeof window !== 'undefined') {
      window.sessionStorage.setItem('sop-site-logout-pending', '1');
      window.localStorage.setItem('sop-site-logout-pending', '1');
    }
    try {
      await logoutFromServer();
    } catch {
      // Continue with client-side logout even if the server request fails.
    } finally {
      console.info('logout cleanup running');
      setAccount(emptyState);
      clearPersistedAccountState();
      clearPendingCheckoutSessionID();
      markSignedOut();
      setName('');
      setEmail('');
      setPassword('');
      setConfirmPassword('');
      setCurrentPassword('');
      setNewPassword('');
      setNewPasswordConfirm('');
      setCheckoutComplete(false);
      setShowInstallStep(false);
      setStatusMessage('You have been signed out.');
      if (typeof window !== 'undefined') {
        window.location.replace('/site/');
      }
      setIsSubmitting(false);
    }
  };

  const handleRecheckBillingStatus = async () => {
    setIsRefreshingBilling(true);
    isRefreshingRef.current = true;
    setStatusMessage('Checking billing status…');
    try {
      const pendingCheckoutSessionID = loadPendingCheckoutSessionID();
      let resolvedAccount = mergeAccountWithFallback(await loadAccountFromServer(), account, name, email);
      if (pendingCheckoutSessionID) {
        const recoveredAccount = await recoverPendingCheckout(pendingCheckoutSessionID);
        if (recoveredAccount) {
          resolvedAccount = mergeAccountWithFallback(recoveredAccount, account, name, email);
        }
      }

      setAccount(resolvedAccount);
      setName(resolvedAccount.name || name);
      setEmail(resolvedAccount.email || email);
      setShowInstallStep(resolvedAccount.billingStatus === 'active' && resolvedAccount.plan === 'starter');
      setCheckoutComplete(resolvedAccount.billingStatus === 'active' && resolvedAccount.plan === 'starter');
      if (resolvedAccount.billingStatus === 'pending') {
        void pingPendingCheckout();
      }
      if (resolvedAccount.isAuthenticated) {
        persistAccountState(resolvedAccount);
      }
      setStatusMessage(
        resolvedAccount.billingStatus === 'active'
          ? 'Billing status refreshed.'
          : resolvedAccount.isAuthenticated
            ? 'Billing status checked.'
            : 'No active billing session was found to recheck.'
      );
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to recheck billing status.');
    } finally {
      setIsRefreshingBilling(false);
      isRefreshingRef.current = false;
    }
  };

  const isStarterActive = account.billingStatus === 'active' && account.plan === 'starter';
  const isStarterPending = account.billingStatus === 'pending' && account.plan === 'starter';
  const canLaunchDataManager = account.isSystemAdmin;
  const canAccessLocalDaemon = Boolean(account.isAuthenticated) && !account.isSystemAdmin && account.plan !== 'none';
  const primaryActionHref = canLaunchDataManager ? '/app' : '/site/subscribe';
  const primaryActionLabel = canLaunchDataManager ? 'Open Data Manager' : 'Subscribe';
  const secondaryActionHref = canLaunchDataManager ? '/app' : '/site/subscribe';
  const secondaryActionLabel = canLaunchDataManager ? 'Open Data Manager' : 'Review subscription';
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
    ? 'Your AI Copilot access is active and ready to use.'
    : account.billingStatus === 'pending'
      ? 'Your subscription is waiting for payment confirmation.'
      : account.failureReason
        ? `The last checkout ended with: ${account.failureReason}.` 
        : 'No active subscription is currently in place.';

  const isSetupCompleteRedirect = typeof window !== 'undefined' && new URLSearchParams(window.location.search).get('setup') === 'complete';
  const shouldShowDashboard = (account.isAuthenticated || provisioningDone || isSetupCompleteRedirect) && view === 'home';

  if (view === 'subscribe') {
    return (
      <main className="app-shell">
        <section className="hero-card subscription-shell">
          <div className="hero-actions">
            <a className="button button-secondary" href="/site/">Back to overview</a>
            <a className="button button-secondary" href="/site/docs">View product docs</a>
          </div>

          <div className="checkout-grid">
            <div className="checkout-card">
              <p className="eyebrow">Secure checkout</p>
              <h2>Start your Northstar AI Copilot access</h2>
              <p className="muted-copy">
                The offer is simple: one access path to the Northstar AI Copilot experience, with Stores and Spaces that make it easier to organize work, inspect how the AI handled a repository, and review multi-turn workflows.
              </p>
              <p className="muted-copy" style={{ marginTop: '0.75rem' }}>
                Your first month is free. After that, billing starts at $5/month and you can cancel anytime.
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

              <div className="form-stack">
                <div className="inline-fields">
                  <div>
                    <label htmlFor="subscription-name" className="field-label">Username</label>
                    <input
                      id="subscription-name"
                      className="text-input"
                      type="text"
                      value={name}
                      onChange={(event) => setName(event.target.value)}
                      placeholder="Choose a username"
                      autoComplete="username"
                    />
                  </div>
                  <div>
                    <label htmlFor="subscription-email" className="field-label">Email</label>
                    <input
                      id="subscription-email"
                      className="text-input"
                      type="email"
                      value={email}
                      onChange={(event) => setEmail(event.target.value)}
                      placeholder="you@example.com"
                      autoComplete="email"
                    />
                  </div>
                </div>
                {!account.isAuthenticated ? (
                  <div className="inline-fields">
                    <div>
                      <label htmlFor="subscription-password" className="field-label">Password</label>
                      <input
                        id="subscription-password"
                        className="text-input"
                        type="password"
                        value={password}
                        onChange={(event) => setPassword(event.target.value)}
                        placeholder="Choose a password"
                        autoComplete="new-password"
                      />
                    </div>
                    <div>
                      <label htmlFor="subscription-password-confirm" className="field-label">Confirm password</label>
                      <input
                        id="subscription-password-confirm"
                        className="text-input"
                        type="password"
                        value={confirmPassword}
                        onChange={(event) => setConfirmPassword(event.target.value)}
                        placeholder="Re-enter your password"
                        autoComplete="new-password"
                      />
                    </div>
                  </div>
                ) : null}
                <p className="muted-copy">
                  Continue securely through Stripe to complete your subscription. You will be redirected to a hosted checkout page to enter your payment details.
                </p>
                <button type="button" className="button button-primary" onClick={() => { void handleSubscription(); }} disabled={isSubmitting}>
                  {isSubmitting ? 'Working…' : 'Subscribe with Stripe'}
                </button>
              </div>
            </div>

            <div className="checkout-card summary-card">
              <h3>{account.isAuthenticated ? 'Choose a plan' : 'Order summary'}</h3>
              <div className="summary-list">
                <div>
                  <span className="summary-label">Plan</span>
                  <strong>{account.isAuthenticated ? 'Available plans for your account' : 'AI Copilot access'}</strong>
                </div>
                <div>
                  <span className="summary-label">Billing</span>
                  <strong>{account.isAuthenticated ? 'Select from the options below' : '$5/month'}</strong>
                </div>
                <div>
                  <span className="summary-label">Includes</span>
                  <strong>{account.isAuthenticated ? 'Knowledge bases, local workflow, and AI Copilot access' : 'AI Copilot experience'}</strong>
                </div>
              </div>

              {account.isAuthenticated ? (
                <div className="preview-box" style={{ marginTop: '0.75rem' }}>
                  <h4>Available plan</h4>
                  <div className="form-stack">
                    <div className="preview-box" style={{ padding: '0.75rem', marginBottom: '0.5rem' }}>
                      <strong>Starter</strong>
                      <p className="muted-copy" style={{ marginTop: '0.25rem' }}>AI Copilot access for local workflows and knowledge-driven automation.</p>
                      <p className="muted-copy">First month free • $5/month after that</p>
                      <p className="muted-copy">Cancel anytime</p>
                    </div>
                  </div>
                </div>
              ) : null}

              {statusMessage ? <p className="muted-copy">{statusMessage}</p> : null}

              {account.isAuthenticated ? (
                <div className="preview-box">
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.5rem' }}>
                    <h4 style={{ margin: 0 }}>{billingStatusTitle}</h4>
                    <button
                      type="button"
                      title="Recheck billing status"
                      aria-label="Recheck billing status"
                      style={{ border: 'none', background: 'transparent', cursor: 'pointer', padding: 0, fontSize: '1rem' }}
                      onClick={() => { void handleRecheckBillingStatus(); }}
                      disabled={isRefreshingBilling}
                    >
                      ↻
                    </button>
                  </div>
                  <p>{billingStatusBody}</p>
                </div>
              ) : null}

              {checkoutComplete ? (
                <div className="success-banner">
                  <h4>Subscription confirmed</h4>
                  <p>Your AI Copilot access is active, and the local-agent download step is ready so you can start working with the product right away.</p>
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

  if (view === 'success') {
    return (
      <main className="app-shell">
        <section className="hero-card subscription-shell">
          <div className="hero-actions">
            <a className="button button-secondary" href="/site/">Back to overview</a>
            <a className="button button-primary" href="/site/subscribe">Manage subscription</a>
          </div>

          <div className="checkout-grid">
            <div className="checkout-card">
              <p className="eyebrow">Payment complete</p>
              <h2>Your Northstar AI Copilot subscription is on the way</h2>
              <p className="muted-copy">
                Your checkout was completed successfully. We are confirming your subscription and will activate your access as soon as the billing state is updated.
              </p>
              <p className="muted-copy" style={{ marginTop: '0.75rem' }}>
                If your account is already active, you can continue to the dashboard immediately. If not, we will refresh your status shortly.
              </p>
              <div className="form-stack" style={{ marginTop: '1rem' }}>
                <a className="button button-primary" href="/site/">Continue to dashboard</a>
                <button type="button" className="button button-secondary" onClick={() => { void handleDownload(); }} disabled={isSubmitting || account.billingStatus !== 'active'}>
                  {isSubmitting ? 'Preparing…' : 'Download sop-daemon'}
                </button>
                <a className="button button-secondary" href="/site/subscribe">View subscription options</a>
              </div>
            </div>

            <div className="checkout-card summary-card">
              <h3>What happens next</h3>
              <div className="summary-list">
                <div>
                  <span className="summary-label">Status</span>
                  <strong>{account.billingStatus === 'active' ? 'Active' : 'Confirming'}</strong>
                </div>
                <div>
                  <span className="summary-label">Access</span>
                  <strong>{account.plan === 'starter' ? 'Northstar AI Copilot access' : 'Subscription pending'}</strong>
                </div>
                <div>
                  <span className="summary-label">Support</span>
                  <strong>Need help? Contact support.</strong>
                </div>
              </div>
              <div className="preview-box" style={{ marginTop: '0.75rem' }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '0.5rem' }}>
                  <h4 style={{ margin: 0 }}>Billing status</h4>
                  <button
                    type="button"
                    title="Recheck billing status"
                    aria-label="Recheck billing status"
                    style={{ border: 'none', background: 'transparent', cursor: 'pointer', padding: 0, fontSize: '1rem' }}
                    onClick={() => { void handleRecheckBillingStatus(); }}
                    disabled={isRefreshingBilling}
                  >
                    ↻
                  </button>
                </div>
                <p>{account.billingStatus === 'active' ? 'Your subscription is active and ready for use.' : 'We are still confirming your subscription with the billing provider.'}</p>
              </div>
            </div>
          </div>
        </section>
      </main>
    );
  }

  if (view === 'cancel') {
    return (
      <main className="app-shell">
        <section className="hero-card subscription-shell">
          <div className="hero-actions">
            <a className="button button-secondary" href="/site/">Back to overview</a>
            <a className="button button-primary" href="/site/subscribe">Try checkout again</a>
          </div>

          <div className="checkout-grid">
            <div className="checkout-card">
              <p className="eyebrow">Checkout cancelled</p>
              <h2>No problem — you can try again anytime</h2>
              <p className="muted-copy">
                Your checkout was cancelled before payment was completed. Your account remains available, and you can restart the subscription process whenever you are ready.
              </p>
              <div className="form-stack" style={{ marginTop: '1rem' }}>
                <a className="button button-primary" href="/site/subscribe">Retry subscription</a>
                <a className="button button-secondary" href="/site/">Return home</a>
              </div>
            </div>

            <div className="checkout-card summary-card">
              <h3>What you can do next</h3>
              <div className="summary-list">
                <div>
                  <span className="summary-label">Next step</span>
                  <strong>Restart checkout</strong>
                </div>
                <div>
                  <span className="summary-label">Plan</span>
                  <strong>Northstar AI Copilot Starter</strong>
                </div>
                <div>
                  <span className="summary-label">Billing</span>
                  <strong>First month free • $5/month after</strong>
                </div>
              </div>
              <div className="preview-box" style={{ marginTop: '0.75rem' }}>
                <h4>Need help?</h4>
                <p>If you ran into an issue during payment, you can retry checkout or contact support for assistance.</p>
              </div>
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
            <a className="button button-primary" href="/site/subscribe">Start AI Copilot access</a>
          </div>

          <div className="docs-intro">
            <p className="eyebrow">Product documentation</p>
            <h1 className="hero-title">A privacy first modern AI copilot powered by knowledge bases for expert-level automation and coding.</h1>
            <p className="hero-copy">
              Northstar brings knowledge-driven AI to your workflow with local-first privacy. Unlike current AI copilots that store your conversation history on their servers, everything stays on your device. Bring your own API keys for direct control. Load knowledge bases to transform your LLM into a domain expert, visualize your AI interactions with built-in tools, and manage your data locally. Our site only handles login and auth—we don't store your chat history.
            </p>
            <p className="hero-copy" style={{ marginTop: '1rem' }}>
              <strong>Privacy-first fleet learning with PII detection.</strong> Your daemon learns from you either way—stored locally for future runs. Optionally share patterns with automatic PII detection, privacy governance, and full transparency: see exactly what's shared through auditable UI visualization. Per-category opt-in/opt-out controls. Your data, your control, fully auditable.
            </p>
            <p className="hero-copy" style={{ marginTop: '1rem' }}>
              <strong>Real-time vector search on your machine.</strong> Other AI copilots run overnight K-Means clustering in their data centers to optimize code search. Northstar uses nested semantic categories plus a relativity-based Triangulation algorithm to make vectorization a general-purpose infrastructure layer—no waiting, no HQ processing, just real-time intelligence on your local device. It is usable through the UI, the API, LLM management, or tool calls, and it leaves domain users to organize knowledge in the way their world naturally works.
            </p>
            <p className="hero-copy" style={{ marginTop: '1rem' }}>
              <strong>Automated expertise enforcement cuts your AI costs dramatically.</strong> Other copilots generate code and leave you to manually verify it follows best practices—wasting hours and burning tokens on refinement. Our KB Quality system validates code against expertise rules during generation. No manual review cycles. No additional prompts to "fix the error handling" or "follow the style guide." Expertise is built into the generator, saving you time and reducing context usage by up to 70%.
            </p>
            <p className="hero-copy" style={{ marginTop: '1rem' }}>
              Public site: <strong>ai-northstar.com</strong>
            </p>
          </div>

          <div className="docs-grid">
            <article className="feature-card">
              <p className="eyebrow">Knowledge-driven AI copilot</p>
              <h3>What makes Northstar different</h3>
              <ul className="feature-list">
                <li>Load knowledge bases to turn your LLM into a domain expert—medical, mechanical, coding, or any field</li>
                <li><strong>Automated KB Quality validation—expertise rules enforced during code generation, eliminating manual refinement</strong></li>
                <li><strong>Up to 70% reduction in context usage—no costly back-and-forth to fix style, error handling, or best practices</strong></li>
                <li><strong>Live Expertise Dashboard—track which knowledge bases are used per session with real-time statistics</strong></li>
                <li><strong>One-click refinement controls—revalidate with stricter standards or try alternative knowledge bases instantly</strong></li>
                <li>Bring your own API keys—we don't store conversation history, unlike other AI copilots</li>
                <li>Lightweight real-time vector database that runs on your machine—no overnight optimization, no data center processing</li>
                <li>Semantic category trees organize millions of code snippets for instant intelligent search</li>
                <li>Privacy-first fleet learning—respects PII with automatic detection, privacy governance, and user opt-in/opt-out policies</li>
                <li>Built-in auditable visualization—see exactly what's shared, when, and why with full transparency</li>
                <li>Generate higher-quality code that follows architecture best practices and coding standards automatically</li>
              </ul>
              <a className="button button-secondary" href="/site/subscribe">Get started</a>
            </article>

            <article className="feature-card">
              <p className="eyebrow">Automated expertise validation</p>
              <h3>Stop wasting time and money on manual refinement</h3>
              <ul className="feature-list">
                <li><strong>KB Quality system validates code during generation—no manual "fix this code" iterations needed</strong></li>
                <li><strong>Expertise Dashboard shows which knowledge bases drive each AI session with live statistics</strong></li>
                <li><strong>One-click refinement controls: revalidate with stricter rules or try alternative knowledge bases</strong></li>
                <li><strong>Up to 70% reduction in context usage compared to manual refinement workflows</strong></li>
                <li>Best practices are built into the code generator—error handling, naming conventions, architecture patterns</li>
                <li>Automated compliance scoring shows how well generated code follows expertise rules</li>
                <li>No more hours spent enforcing style guides and best practices—it's automatic</li>
              </ul>
            </article>

            <article className="feature-card">
              <p className="eyebrow">Real-time intelligence on your machine</p>
              <h3>Full AI copilot power without the data center</h3>
              <ul className="feature-list">
                <li>Vector database runs in real-time on your local machine—no overnight K-Means optimization needed</li>
                <li>Other copilots send your code to data centers for clustering and search—ours works instantly on-device</li>
                <li>Semantic M-Way tree buckets organize code by concepts: error-handling, retry-logic, data-persistence</li>
                <li>Three-index strategy: file paths (O(1)), code structure (O(1)), semantic concepts (O(D log N))</li>
                <li>Privacy governance with PII detection—respects privacy concerns with automatic filtering and user controls</li>
                <li>We don't store your conversation history—everything stays local on your device</li>
                <li>Built-in auditable visualization—full transparency on AI workflows and fleet participation</li>
              </ul>
            </article>
          </div>

          <div className="docs-grid docs-grid-wide">
            <article className="feature-card">
              <p className="eyebrow">Better coding workflows</p>
              <h3>Concrete improvements to your daily work</h3>
              <div className="capability-list">
                <div>
                  <strong>Automated expertise validation</strong>
                  <p>Generate code that follows best practices automatically—no manual refinement cycles, no wasted hours enforcing standards. The KB Quality system validates during generation, saving up to 70% in context costs.</p>
                </div>
                <div>
                  <strong>Pair programming with built-in expertise</strong>
                  <p>Code alongside an AI that understands architecture patterns and best practices from your knowledge base. Expertise rules are enforced automatically as code is generated.</p>
                </div>
                <div>
                  <strong>Expert code reviews with live insights</strong>
                  <p>Get reviews that check against your coding standards and architectural guidelines, not just generic rules. The Expertise Dashboard shows which knowledge bases contributed to each session.</p>
                </div>
                <div>
                  <strong>Task automation with quality guarantees</strong>
                  <p>Automate repetitive work with an AI that knows your domain AND validates output automatically, cutting down SDLC while ensuring quality standards are met.</p>
                </div>
              </div>
            </article>

            <article className="feature-card">
              <p className="eyebrow">Real-world use cases</p>
              <h3>From domain experts to practical automation</h3>
              <ul className="feature-list">
                <li>Load medical knowledge bases so your LLM answers questions like a doctor</li>
                <li>Turn your AI into a mechanic, lawyer, or any expert with the right knowledge base</li>
                <li>Let the LLM manage your data in Spaces, or manage it yourself—both benefit from semantic search</li>
                <li>Review multi-turn AI workflows to see how knowledge was applied to your project</li>
              </ul>
            </article>
          </div>

          <article className="feature-card">
            <p className="eyebrow">The complete experience</p>
            <h3>Knowledge bases, automated expertise, and real-time AI</h3>
            <p className="muted-copy">
              Current AI copilots can't do this: load domain expertise, <strong>automatically validate code against best practices during generation</strong>, keep your conversation history on your device, AND run intelligent code search in real-time without data center processing. Other copilots generate code and leave you to manually fix compliance issues, wasting hours and burning tokens. Northstar's KB Quality system enforces expertise automatically—no manual refinement cycles needed. This cuts context usage by up to 70% and saves you from spending hours enforcing style guides, error handling patterns, and architectural standards.
            </p>
            <ul className="feature-list">
              <li><strong>Automated KB Quality validation—expertise rules enforced during generation, eliminating manual refinement iterations.</strong></li>
              <li><strong>Massive cost savings—up to 70% reduction in context usage compared to manual "fix this code" workflows.</strong></li>
              <li><strong>Live Expertise Dashboard—see which knowledge bases contribute to each session with real-time statistics.</strong></li>
              <li><strong>One-click refinement controls—revalidate with stricter standards or try alternative knowledge bases instantly.</strong></li>
              <li>Knowledge bases transform your LLM into a domain expert with higher-quality output automatically validated.</li>
              <li>Real-time vector database runs on your local machine—no overnight clustering, no HQ processing delays.</li>
              <li>Semantic M-Way tree buckets organize millions of code snippets for instant intelligent search.</li>
              <li>Three-index approach: file paths (exact), code structure (package/class/method), semantic concepts (fuzzy).</li>
              <li>Privacy governance with PII detection—respects privacy concerns with automatic filtering and user opt-in/opt-out policies.</li>
              <li>Bring your own API keys—we don't store conversation history, unlike other AI copilots.</li>
              <li>Built-in auditable visualization—full transparency on data flows and exactly what's shared.</li>
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
                <h3>Load knowledge bases</h3>
                <p>Turn your LLM into a domain expert by loading knowledge bases for any field—coding, medicine, mechanics, or more.</p>
              </div>
            </div>
            <div className="journey-step">
              <span className="journey-number">3</span>
              <div>
                <h3>Import KB JSON into Spaces</h3>
                <p>
                  Compile your curated markdown knowledge bases into JSON format and import them into Spaces for AI-assisted development. 
                  Run <code>go run ./ai/cmd/knowledge_compiler -input path/to/kb.md</code> to generate the JSON, 
                  then use the Spaces Import feature to load it. The KB compiler validates structure, extracts metadata, 
                  and prepares content for semantic search and RAG workflows.
                </p>
                <p style={{ marginTop: '0.75rem' }}>
                  For the supported authoring format, structure the markdown with <strong>#</strong>, <strong>##</strong>, and optional <strong>###</strong> headings to define semantic categories, then place each knowledge entry as an explicit item block beneath the most relevant heading. Each block should include a short title, 1–5 <code>Summary:</code> lines, a <code>Body:</code> section, and <code>Source:</code> or <code>Sources:</code> provenance. A short paragraph directly under a category heading becomes the category description.
                </p>
                <p style={{ marginTop: '0.75rem' }}>
                  A practical workflow is: start with the curation contract in <code>lib/sop/ai/KB_CURATION_MANIFEST.md</code>, draft one curated markdown file, then run the KB compiler to generate the importable JSON. The compiler expects a curated markdown input file via <code>-input</code>, and the generated JSON is what you import into Spaces.
                </p>
                <p style={{ marginTop: '0.75rem' }}>
                  Example structure:
                </p>
                <pre style={{ marginTop: '0.5rem', padding: '0.75rem', backgroundColor: '#f8f9fa', borderRadius: '6px', overflowX: 'auto', fontSize: '0.85rem' }}>{`# Platform Foundations

## Architecture

This category covers backend topology and system design.

- Item: Filesystem and hybrid backend model
  Summary: SOP supports a filesystem backend and a hybrid metadata backend.
  Body:
  The system can operate with a filesystem-first model or a hybrid model for larger deployments.
  Sources: ARCHITECTURE.md, README.md
`}</pre>
                <p style={{ marginTop: '0.75rem' }}>
                  Follow the manifest closely, keep the taxonomy bounded, merge duplicates, and keep each item concise, factual, and provenance-backed so the compiler can turn your markdown into a clean Import JSON artifact.
                </p>
              </div>
            </div>
            <div className="journey-step">
              <span className="journey-number">4</span>
              <div>
                <h3>Automate with expert AI</h3>
                <p>Let the knowledge-guided AI copilot generate better code, review work, and automate tasks at an expert level.</p>
              </div>
            </div>
          </div>

          <article className="feature-card" style={{ marginTop: '1rem', marginBottom: '2rem' }}>
            <p className="eyebrow">Subscribed user documentation</p>
            <h3>Knowledge Base and Space curation workflow</h3>
            <p className="muted-copy">
              This workflow is part of the subscribed user-facing product documentation. It shows how to curate markdown, compile it into importable JSON, and bring that knowledge into Spaces.
            </p>
            <div className="journey-step" style={{ marginTop: '1rem', background: 'transparent', border: '1px solid var(--border)' }}>
              <span className="journey-number">3</span>
              <div>
                <h3>Import KB JSON into Spaces</h3>
                <p>
                  Compile your curated markdown knowledge bases into JSON format and import them into Spaces for AI-assisted development. 
                  Run <code>go run ./ai/cmd/knowledge_compiler -input path/to/kb.md</code> to generate the JSON, 
                  then use the Spaces Import feature to load it. The KB compiler validates structure, extracts metadata, 
                  and prepares content for semantic search and RAG workflows.
                </p>
                <p style={{ marginTop: '0.75rem' }}>
                  For the supported authoring format, structure the markdown with <strong>#</strong>, <strong>##</strong>, and optional <strong>###</strong> headings to define semantic categories, then place each knowledge entry as an explicit item block beneath the most relevant heading. Each block should include a short title, 1–5 <code>Summary:</code> lines, a <code>Body:</code> section, and <code>Source:</code> or <code>Sources:</code> provenance. A short paragraph directly under a category heading becomes the category description.
                </p>
                <p style={{ marginTop: '0.75rem' }}>
                  A practical workflow is: start with the curation contract in <code>lib/sop/ai/KB_CURATION_MANIFEST.md</code>, draft one curated markdown file, then run the KB compiler to generate the importable JSON. The compiler expects a curated markdown input file via <code>-input</code>, and the generated JSON is what you import into Spaces.
                </p>
                <p style={{ marginTop: '0.75rem' }}>
                  Example structure:
                </p>
                <pre style={{ marginTop: '0.5rem', padding: '0.75rem', backgroundColor: '#f8f9fa', borderRadius: '6px', overflowX: 'auto', fontSize: '0.85rem' }}>{`# Platform Foundations

## Architecture

This category covers backend topology and system design.

- Item: Filesystem and hybrid backend model
  Summary: SOP supports a filesystem backend and a hybrid metadata backend.
  Body:
  The system can operate with a filesystem-first model or a hybrid model for larger deployments.
  Sources: ARCHITECTURE.md, README.md
`}</pre>
                <p style={{ marginTop: '0.75rem' }}>
                  Follow the manifest closely, keep the taxonomy bounded, merge duplicates, and keep each item concise, factual, and provenance-backed so the compiler can turn your markdown into a clean Import JSON artifact.
                </p>
              </div>
            </div>
          </article>

          <article id="sdk-documentation" className="feature-card" style={{ marginTop: '1rem', marginBottom: '2rem', backgroundColor: '#f8f9fa', border: '2px solid #e9ecef' }}>
            <p className="eyebrow">SDK documentation</p>
            <h3>Public SDK documentation preview: vector database architecture and AI database platform</h3>
            <p className="muted-copy">
              This section focuses on the SDK story itself: the new vector database architecture, the comparison to legacy centroid-based systems, and the AI database platform that powers it.
            </p>
            <p className="muted-copy" style={{ marginTop: '0.75rem' }}>
              Traditional vector databases require overnight K-Means clustering in data centers to organize embeddings. The new SDK architecture replaces that with <strong>nested semantic categories</strong> and a <strong>relativity-based Triangulation algorithm</strong>, turning vectorization into a <strong>general-purpose infrastructure capability</strong> that is usable through the <strong>UI, API, LLM management, or tool calls</strong>.
            </p>

            <div style={{ marginTop: '2rem', marginBottom: '2rem', padding: '1.5rem', backgroundColor: '#fff', borderRadius: '8px', border: '1px solid #dee2e6' }}>
              <h4 style={{ marginTop: 0, marginBottom: '1rem', fontSize: '1.1rem' }}>Traditional vs SOP Architecture</h4>
              <pre style={{ fontSize: '0.85rem', lineHeight: '1.6', overflow: 'auto', backgroundColor: '#f8f9fa', padding: '1rem', borderRadius: '4px', margin: 0 }}>{`
┌─────────────────────────────────────────────────────────┐
│          TRADITIONAL VECTOR DATABASE                     │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  1. Store embeddings in flat array                      │
│     [vec1, vec2, vec3, ... vecN]                        │
│                                                          │
│  2. Run K-Means clustering (OVERNIGHT)                  │
│     ↓ Send to data center                               │
│     ↓ Compute centroids                                 │
│     ↓ Assign clusters                                   │
│                                                          │
│  3. Query uses centroid distance                        │
│     - Compare to cluster centers                        │
│     - Scan cluster members                              │
│     - Return opaque "Cluster 47" results               │
│                                                          │
│  ❌ Requires data scientists                            │
│  ❌ Overnight batch processing                          │
│  ❌ Opaque cluster IDs                                  │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│          SOP REAL-TIME VECTOR DATABASE                   │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  1. Organize in nested semantic categories               │
│     Go Best Practices/                                   │
│     ├── error-handling/                                 │
│     │   ├── retry-logic/                                │
│     │   │   ├── exponential-backoff                     │
│     │   │   └── circuit-breaker                         │
│     │   └── error-propagation/                          │
│     └── concurrency-patterns/                           │
│         └── goroutine-management/                       │
│                                                          │
│  2. Triangulation search (REAL-TIME)                    │
│     ↓ Navigate category tree                            │
│     ↓ Relativity-based scoring                          │
│     ↓ Instant results on your machine                   │
│                                                          │
│  3. Query returns human-readable paths                  │
│     - "error-handling/retry-logic"                     │
│     - O(D log N) performance                            │
│     - No data center processing                         │
│                                                          │
│  ✅ End users organize categories                       │
│  ✅ Real-time updates                                   │
│  ✅ Transparent results                                 │
└─────────────────────────────────────────────────────────┘
                `}</pre>
            </div>

            <div style={{ marginTop: '2rem', marginBottom: '2rem', padding: '1.5rem', backgroundColor: '#fff', borderRadius: '8px', border: '1px solid #dee2e6' }}>
              <h4 style={{ marginTop: 0, marginBottom: '1rem', fontSize: '1.1rem' }}>Triangulation Algorithm Visualization</h4>
              <pre style={{ fontSize: '0.85rem', lineHeight: '1.6', overflow: 'auto', backgroundColor: '#f8f9fa', padding: '1rem', borderRadius: '4px', margin: 0 }}>{`
Query: "How to handle retries with backoff?"
           ↓
   [Convert to embedding]
           ↓
┌──────────────────────────────────────────────────┐
│         Category Tree Navigation                 │
└──────────────────────────────────────────────────┘

    Root (Space)
        │
        ├─→ error-handling (0.89 similarity) ✓
        │       │
        │       ├─→ retry-logic (0.94 similarity) ✓✓
        │       │       │
        │       │       ├─→ exponential-backoff (0.96) ⭐
        │       │       ├─→ circuit-breaker (0.78)
        │       │       └─→ timeout-strategies (0.71)
        │       │
        │       └─→ error-propagation (0.65)
        │
        ├─→ concurrency-patterns (0.54)
        └─→ testing-practices (0.31)

Triangulation Process:
  1. Start at root, score all top-level categories
  2. Select best match: error-handling (0.89)
  3. Traverse deeper: retry-logic (0.94)
  4. Score leaf nodes: exponential-backoff (0.96) ⭐
  5. Check boundary categories for cross-cutting
  6. Return: error-handling/retry-logic/exponential-backoff

Result: Found in O(D log N) time, no K-Means needed!
                `}</pre>
            </div>

            <div className="capability-list">
              <div>
                <strong>Nested Semantic Categories</strong>
                <p>Instead of blind clustering, organize embeddings into human-readable nested categories (e.g., error-handling → retry-logic → exponential-backoff). End users curate domain organization through intuitive category hierarchies, eliminating the need for specialized data science teams.</p>
              </div>
              <div>
                <strong>Relativity-based Triangulation</strong>
                <p>Our triangulation algorithm performs fuzzy vector searches in O(D log N) time by navigating the semantic category tree. No K-Means centroids, no overnight batch jobs—just instant intelligent search using category relationships and embedding proximity.</p>
              </div>
              <div>
                <strong>Data science in the database</strong>
                <p>Vectorization and fuzzy search are core database primitives, not external analytics layers. Users organize knowledge through categories they understand, and the system handles embeddings, similarity scoring, and retrieval automatically. Data scientists are no longer required; domain experts become the curators of the structure.</p>
              </div>
              <div>
                <strong>User-driven organization</strong>
                <p>Domain experts own the taxonomy of their field. Medical experts create medical categories, developers create architecture patterns, and the system adapts instantly to human understanding—without the need for data scientists to translate knowledge into batch clustering jobs.</p>
              </div>
            </div>

            <div style={{ marginTop: '2rem', padding: '1rem', backgroundColor: '#e7f3ff', borderLeft: '4px solid #0066cc', borderRadius: '4px' }}>
              <p style={{ margin: 0, fontSize: '0.95rem' }}>
                <strong>💡 Key Insight:</strong> By making vectorization and semantic search core database primitives with user-driven category organization, we eliminate the need for specialized data science teams. The relativity-based Triangulation architecture solves the infrastructure problem elegantly, while domain experts organize knowledge the way they naturally think about it and the system handles retrieval automatically through the UI, API, LLM management, or tool calls.
              </p>
            </div>
          </article>

          <article className="feature-card" style={{ marginTop: '1rem', marginBottom: '2rem' }}>
            <p className="eyebrow">AI database platform</p>
            <h3>Lightweight, fast, and purpose-built for AI workloads</h3>
            <p className="muted-copy">
              Northstar includes an AI database that is deliberately lightweight and just right for the needs of modern AI systems. It is built around <strong>ACID transactional B-trees</strong> for dependable updates, strong consistency, and efficient record handling, without the overhead of a heavyweight enterprise database.
            </p>
            <div className="capability-list" style={{ marginTop: '1rem' }}>
              <div>
                <strong>ACID transactions</strong>
                <p>Reliable, transactional data operations for AI pipelines, knowledge updates, and workflow state changes—without sacrificing speed.</p>
              </div>
              <div>
                <strong>Fast and optimal by design</strong>
                <p>The engine is tuned to be lightweight, efficient, and practical for AI-centric data platforms, keeping the stack simple and responsive.</p>
              </div>
              <div>
                <strong>Built-in software replication with erasure coding</strong>
                <p>Data durability and resilience are handled natively through software replication and erasure coding, supporting dependable operation at different scales.</p>
              </div>
              <div>
                <strong>Supports large data types</strong>
                <p>From structured knowledge to large blobs and media files, the platform is designed to handle the broader data needs of AI applications and rich content workflows.</p>
              </div>
              <div>
                <strong>Built-in L1/L2 caching</strong>
                <p>High-performance caching is part of the platform design, reducing latency and making repeated AI access patterns faster and more efficient.</p>
              </div>
              <div>
                <strong>Swarm computing architecture</strong>
                <p>The system is designed for collaborative, distributed execution patterns that scale naturally across nodes in the cluster while staying coordinated and lightweight.</p>
              </div>
              <div>
                <strong>Pheromone-based minimal coordination</strong>
                <p>Intelligent coordination is achieved through lightweight pheromone-inspired signaling, enabling fully autonomous operation in the cluster where serverless means every node acts as a server, with only light coordination where needed.</p>
              </div>
            </div>
          </article>

          <div className="docs-comparison">
            <article className="feature-card comparison-card">
              <p className="eyebrow">Real-time on your machine</p>
              <h3>No overnight optimization, no data center delays</h3>
              <p className="muted-copy">Other AI copilots run K-Means clustering overnight in their data centers. Northstar uses semantic category trees for instant intelligent search—everything runs in real-time on your local device.</p>
            </article>
            <article className="feature-card comparison-card">
              <p className="eyebrow">Knowledge-driven AI</p>
              <h3>Go beyond current AI copilots</h3>
              <p className="muted-copy">Turn your LLM into a domain expert with knowledge bases. Generate higher-quality code and get expert answers.</p>
            </article>
            <article className="feature-card comparison-card">
              <p className="eyebrow">Shorter SDLC</p>
              <h3>Cut development time</h3>
              <p className="muted-copy">Knowledge-guided workflows automate tasks at an expert level, following best practices and architectural patterns.</p>
            </article>
          </div>
        </section>
      </main>
    );
  }

  if (shouldShowDashboard) {
    return (
      <main className="app-shell">
        <section className="hero-card">
          <div className="hero-grid">
            <div>
              <p className="eyebrow">Northstar workspace</p>
              <h1 className="hero-title">Welcome back to the new site dashboard</h1>
              <p className="hero-copy">
                Setup is complete. Review your account, keep your subscription current, and open the SOP Data Manager whenever you need the admin-level operational view.
              </p>
              <div className="hero-actions">
                <a className="button button-primary" href={primaryActionHref}>{primaryActionLabel}</a>
                {account.isAuthenticated ? (
                  <button type="button" className="button button-secondary" onClick={handleLogout} disabled={isSubmitting}>
                    {isSubmitting ? 'Working…' : 'Log out'}
                  </button>
                ) : null}
                {canAccessLocalDaemon ? (
                  <a className="button button-secondary" href="http://127.0.0.1:9090" target="_blank" rel="noreferrer">Open local workflow</a>
                ) : null}
                <a className="button button-secondary" href="/site/docs">View product docs</a>
              </div>
            </div>

            <div className="hero-side">
              <div className="plan-card">
                <p className="eyebrow">Account overview</p>
                <h3>{account.name || 'Your account'}</h3>
                <p>{account.email || 'Your email will appear here once the account is linked.'}</p>
                <div className="summary-list">
                  <div>
                    <span className="summary-label">Status</span>
                    <strong>{account.isAuthenticated ? 'Signed in' : 'Pending sign-in'}</strong>
                  </div>
                  <div>
                    <span className="summary-label">Plan</span>
                    <strong>{account.plan === 'starter' ? 'AI Copilot access' : account.plan === 'hq' ? 'HQ' : 'Not subscribed'}</strong>
                  </div>
                  <div>
                    <span className="summary-label">Access</span>
                    <strong>{canLaunchDataManager ? 'Admin access' : 'Standard user'}</strong>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </section>

        {!canLaunchDataManager ? (
          <section className="hero-card secondary-card">
            <div className="stack-grid">
              <div className="form-panel">
                <h2>Next steps</h2>
                <p className="muted-copy">
                  Continue with local-agent setup, review your subscription, or open the SOP Data Manager when you need deeper operator controls.
                </p>
                <div className="action-row">
                  <a className="button button-secondary" href={secondaryActionHref}>{secondaryActionLabel}</a>
                  <a className="button button-secondary" href="/site/docs">Product docs</a>
                </div>
              </div>

              <div className="summary-panel">
                <h3>Workspace summary</h3>
                <div className="summary-list">
                  <div>
                    <span className="summary-label">Subscription</span>
                    <strong>{isStarterActive ? 'Active' : isStarterPending ? 'Pending' : 'Not subscribed'}</strong>
                  </div>
                  <div>
                    <span className="summary-label">Local agent</span>
                    <strong>{isStarterActive ? 'Ready to download' : 'Pending activation'}</strong>
                  </div>
                  <div>
                    <span className="summary-label">Data Manager</span>
                    {canLaunchDataManager ? (
                      <a href="/app" className="summary-link">Open Data Manager</a>
                    ) : (
                      <strong>Admin access required</strong>
                    )}
                  </div>
                </div>
              </div>
            </div>
          </section>
        ) : null}
      </main>
    );
  }

  return (
    <main className="app-shell">
      <section className="hero-card">
        <div className="hero-grid">
          <div>
            <p className="eyebrow">Northstar</p>
            <h1 className="hero-title">A privacy first modern AI copilot powered by knowledge bases</h1>
            <p className="hero-copy">
              Unlike current AI copilots that store your conversation history on their servers and run overnight optimization in data centers, Northstar keeps everything on your device with real-time intelligence. Bring your own API keys for direct control. Load knowledge bases to turn your LLM into a domain expert, get higher-quality code, and manage your data locally with built-in visualization. We don't store your chat history—our site only handles auth.
            </p>
            <p className="hero-copy" style={{ marginTop: '1rem' }}>
              <strong>Privacy-first fleet learning with PII detection and governance.</strong> Your daemon learns locally either way. Optionally share patterns with automatic PII detection, privacy governance, and full auditable transparency—see exactly what's shared with per-category opt-in/opt-out controls.
            </p>
            <p className="hero-copy" style={{ marginTop: '1rem' }}>
              <strong>Full AI copilot features, zero data center processing.</strong> Other copilots need overnight K-Means clustering at HQ. Ours uses semantic category trees that work instantly on your machine.
            </p>
            <p className="hero-copy" style={{ marginTop: '1rem' }}>
              <strong>Built-in expertise validation saves you time and money.</strong> Unlike other copilots that require manual refinement cycles, our automated KB Quality system validates code against best practices during generation—no additional prompts, no costly back-and-forth iterations. Expertise is enforced automatically, cutting context usage by up to 70%.
            </p>
            <div className="hero-actions">
              <a className="button button-primary" href={primaryActionHref}>{primaryActionLabel}</a>
              <a className="button button-secondary" href="/site/docs">View product docs</a>
              <a className="button button-secondary" href="/site/docs#sdk-documentation">Coming soon</a>
            </div>
          </div>

          <div className="hero-side" style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(320px, 1fr))', gap: '1.5rem' }}>
            <div className="plan-card">
              <p className="eyebrow">AI Copilot access</p>
              <h3>Knowledge-driven AI for smarter automation and coding</h3>
              <div className="price">$5/month</div>
              <p>One simple plan unlocks the modern AI copilot experience with knowledge base capabilities and real-time vector search that current AI copilots don't have.</p>
              <ul className="feature-list">
                <li>Load knowledge bases to turn your LLM into a domain expert in any field</li>
                <li>Real-time vector database on your machine—no overnight optimization or HQ processing</li>
                <li>Semantic category trees organize millions of code snippets for instant intelligent search</li>
                <li><strong>Automated KB Quality validation—expertise enforced during code generation, no manual refinement needed</strong></li>
                <li><strong>Massive context savings—built-in validation eliminates costly back-and-forth iterations, reducing token usage by up to 70%</strong></li>
                <li><strong>Expertise Dashboard—real-time visibility into which knowledge bases drive your AI sessions</strong></li>
                <li><strong>Smart refinement controls—instantly revalidate code or try alternative knowledge bases with one click</strong></li>
                <li>Privacy-first fleet learning—PII detection, privacy governance, and per-category opt-in/opt-out controls</li>
                <li>Full transparency with auditable UI—see exactly what's shared, when, and why with complete visibility</li>
                <li>Bring your own API keys—we don't store your conversation history on our servers</li>
                <li>Built-in data visualization to review your AI workflows and fleet participation</li>
                <li>Generate higher-quality code that adheres to architecture and coding best practices automatically</li>
                <li>Everything stored locally on your device (data privacy)</li>
              </ul>
            </div>

          </div>
        </div>
      </section>

      <section className="hero-card secondary-card">
        <div className="stack-grid">
          <div className="summary-panel" style={{ gridColumn: '1 / -1' }}>
            <h3>Account summary</h3>
            <div className="summary-list">
              <div>
                <span className="summary-label">Status</span>
                <strong>{isStarterActive ? 'AI Copilot access active' : isStarterPending ? 'Checkout pending' : 'Not subscribed'}</strong>
              </div>
              <div>
                <span className="summary-label">Plan</span>
                <strong>{isStarterActive ? 'AI Copilot access' : isStarterPending ? 'AI Copilot access (pending)' : 'No active access'}</strong>
              </div>
              <div>
                <span className="summary-label">Included</span>
                <strong>AI Copilot, Knowledge Bases, Data Stores</strong>
              </div>
            </div>

            <div className="preview-box" style={{ marginTop: '1rem' }}>
              <div className="stack-grid" style={{ gap: '0.75rem' }}>
                <div>
                  <h4 style={{ marginTop: 0, marginBottom: '0.25rem' }}>Account management</h4>
                  <p className="muted-copy" style={{ margin: 0 }}>Manage sign-in, billing, and local access from one place.</p>
                </div>
                <div className="action-row">
                  {!account.isAuthenticated ? (
                    <button
                      type="button"
                      className="button button-secondary"
                      onClick={() => { void handleLogin(); }}
                      disabled={isSubmitting}
                    >
                      Log in
                    </button>
                  ) : null}
                  {account.isAuthenticated ? (
                    <button type="button" className="button button-secondary" onClick={handleLogout} disabled={isSubmitting}>
                      Log out
                    </button>
                  ) : null}
                  {canAccessLocalDaemon ? (
                    <a className="button button-secondary" href="http://127.0.0.1:9090" target="_blank" rel="noreferrer">Open local workflow</a>
                  ) : null}
                  {account.isAuthenticated && account.plan !== 'none' ? (
                    <button type="button" className="button button-secondary" onClick={handleUnsubscribe} disabled={isSubmitting}>
                      Unsubscribe
                    </button>
                  ) : null}
                  {account.isAuthenticated ? (
                    <button type="button" className="button button-secondary danger" onClick={handleUnregister} disabled={isSubmitting}>
                      Unregister
                    </button>
                  ) : null}
                </div>
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

            {!account.isAuthenticated ? null : (
              <div className="form-stack" style={{ marginTop: '1rem' }}>
                <h4>Change password</h4>
                <label htmlFor="current-password" className="field-label">Current password</label>
                <input id="current-password" className="text-input" type="password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} placeholder="Current password" autoComplete="current-password" />
                <label htmlFor="new-password" className="field-label">New password</label>
                <input id="new-password" className="text-input" type="password" value={newPassword} onChange={(event) => setNewPassword(event.target.value)} placeholder="New password" autoComplete="new-password" />
                <label htmlFor="new-password-confirm" className="field-label">Confirm new password</label>
                <input id="new-password-confirm" className="text-input" type="password" value={newPasswordConfirm} onChange={(event) => setNewPasswordConfirm(event.target.value)} placeholder="Confirm new password" autoComplete="new-password" />
                <button type="button" className="button button-secondary" onClick={handlePasswordChange} disabled={isSubmitting}>
                  {isSubmitting ? 'Working…' : 'Update password'}
                </button>
              </div>
            )}

            {statusMessage ? <p className="muted-copy" style={{ marginTop: '0.75rem' }}>{statusMessage}</p> : null}

            {shouldShowInstallPanel ? (
              <div className="preview-box install-box">
                <h4>Next step: install the local agent</h4>
                <p>Your AI Copilot access is active, so the local agent is ready to install. Download it from the official SOP releases page and pair it with the AI copilot to launch the host-side workflow.</p>
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
