import { useEffect, useState, useRef, type MouseEvent } from 'react';

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
  configProvisioned: boolean;
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
  configProvisioned: false,
};

const DRAFT_STORAGE_KEY = 'sop-cloud-site-draft';
const ACCOUNT_STATE_STORAGE_KEY = 'sop-cloud-site-account-state';
const PROVISIONING_DONE_STORAGE_KEY = 'sop_provisioning_done';
const SIGNED_OUT_STORAGE_KEY = 'sop-site-signed-out';
const PENDING_CHECKOUT_SESSION_STORAGE_KEY = 'sop-cloud-site-pending-checkout-session';
const THEME_STORAGE_KEY = 'theme';
const DEFAULT_DAEMON_URL = 'http://127.0.0.1:9090';

type ThemeMode = 'light' | 'dark';

function resolveThemePreference(): ThemeMode {
  if (typeof window === 'undefined') {
    return 'light';
  }
  try {
    const storedTheme = window.localStorage.getItem(THEME_STORAGE_KEY);
    if (storedTheme === 'light' || storedTheme === 'dark') {
      return storedTheme;
    }
  } catch {
    // Fall back to the system preference.
  }
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyThemePreference(theme: ThemeMode) {
  if (typeof window === 'undefined') {
    return;
  }
  document.documentElement.setAttribute('data-theme', theme);
  document.documentElement.style.colorScheme = theme;
  document.body.setAttribute('data-theme', theme);
}

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

function clearBrowserSessionCookies() {
  if (typeof document === 'undefined') {
    return;
  }
  const cookieNames = ['sop_access_token', 'sop_refresh_token', 'sop_session_token', 'access_token', 'sop_userId'];
  for (const cookieName of cookieNames) {
    document.cookie = `${cookieName}=; expires=Thu, 01 Jan 1970 00:00:00 GMT; path=/`;
  }
}

function clearPersistedAccountState() {
  if (typeof window === 'undefined') {
    return;
  }
  window.localStorage.removeItem(ACCOUNT_STATE_STORAGE_KEY);
  window.localStorage.removeItem(DRAFT_STORAGE_KEY);
  window.sessionStorage.removeItem(ACCOUNT_STATE_STORAGE_KEY);
  window.localStorage.removeItem('sop_access_token');
  window.localStorage.removeItem('sop_refresh_token');
  window.localStorage.removeItem('sop_session_token');
  window.localStorage.removeItem('access_token');
  window.localStorage.removeItem('sop_userId');
  window.sessionStorage.removeItem('sop_access_token');
  window.sessionStorage.removeItem('sop_refresh_token');
  window.sessionStorage.removeItem('sop_session_token');
  window.sessionStorage.removeItem('access_token');
  window.sessionStorage.removeItem('sop_userId');
  // Clear daemon-specific tokens to prevent state corruption after mode switching
  window.localStorage.removeItem('sop_daemon_access_token');
  window.localStorage.removeItem('sop_daemon_refresh_token');
  window.localStorage.removeItem('sop_daemon_token_expiry');
  window.localStorage.removeItem('sop_last_subscription_check_timestamp');
  window.localStorage.removeItem('sop_chat_runtime');
  window.localStorage.removeItem('sop_daemon_url');
  clearBrowserSessionCookies();
}

function clearAIStudioHQTokens() {
  if (typeof window === 'undefined') {
    return;
  }
  // Clear HQ mode tokens only (used when opening AI Studio in HQ mode)
  // This prevents stale HQ tokens from conflicting with new session
  window.localStorage.removeItem('sop_access_token');
  window.localStorage.removeItem('sop_refresh_token');
  window.localStorage.removeItem('sop_session_token');
  window.localStorage.removeItem('access_token');
  window.localStorage.removeItem('sop_userId');
  window.sessionStorage.removeItem('sop_access_token');
  window.sessionStorage.removeItem('sop_refresh_token');
  window.sessionStorage.removeItem('sop_session_token');
  window.sessionStorage.removeItem('access_token');
  window.sessionStorage.removeItem('sop_userId');
}

function clearAIStudioDaemonTokens() {
  if (typeof window === 'undefined') {
    return;
  }
  // Clear daemon mode tokens only (used when opening AI Studio in daemon mode)
  // This prevents stale daemon tokens from conflicting with new daemon session
  window.localStorage.removeItem('sop_daemon_access_token');
  window.localStorage.removeItem('sop_daemon_refresh_token');
  window.localStorage.removeItem('sop_daemon_token_expiry');
  window.localStorage.removeItem('sop_chat_runtime');
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
  window.localStorage.removeItem('sop-site-logout-pending');
  window.sessionStorage.removeItem('sop-site-logout-pending');
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

function isLogoutPending(): boolean {
  if (typeof window === 'undefined') {
    return false;
  }
  return window.sessionStorage.getItem('sop-site-logout-pending') === '1' || window.localStorage.getItem('sop-site-logout-pending') === '1' || window.sessionStorage.getItem(SIGNED_OUT_STORAGE_KEY) === '1' || window.localStorage.getItem(SIGNED_OUT_STORAGE_KEY) === '1';
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
  if (pathname.startsWith('/site/subscribe/fail')) {
    return 'cancel';
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
  if (trimmedName.includes('@')) {
    return trimmedName;
  }
  if (trimmedName === 'root' || trimmedName.toLowerCase() === 'sa' || trimmedName.toLowerCase() === 'systemadmin' || trimmedName.toLowerCase() === 'system admin') {
    return trimmedName;
  }
  if (trimmedName) {
    const baseName = slugify(trimmedName);
    if (baseName && baseName !== 'sop-user') {
      return baseName;
    }
  }
  if (trimmedEmail) {
    const emailBase = slugify(trimmedEmail.split('@')[0] || trimmedEmail);
    if (emailBase && emailBase !== 'sop-user') {
      return emailBase;
    }
  }
  return `guest-${createTemporarySuffix()}`;
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
    const user = payload?.user;
    if (!user || typeof user !== 'object') {
      return null;
    }
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
      isAuthenticated: Boolean(user.username),
      isSystemAdmin: canAccessDataManager,
      checkoutUrl: billing.checkout_url ?? '',
      lastWebhookEvent: billing.last_webhook_event ?? '',
      failureReason: billing.failure_reason ?? '',
      lastWebhookAt: billing.last_webhook_at ?? '',
      payments: [],
      subscriptions: [],
      configProvisioned: true,
    };
  } catch {
    return null;
  }
}

async function loadAccountFromServer(): Promise<AccountState> {
  if (isLogoutPending()) {
    return emptyState;
  }
  const response = await fetch('/api/auth/status', { credentials: 'include' });
  if (!response.ok) {
    return emptyState;
  }
  const payload = await response.json();

  const user = payload?.user ?? {};
  const userHasIdentity = Boolean(user.username || user.email || user.role || user.subscription_plan || user.billing_status || user.billing);
  const configProvisioned = payload?.config_provisioned !== false;
  
  // If no user identity exists, return unauthenticated state
  if (!userHasIdentity) {
    return {
      ...emptyState,
      configProvisioned,
    };
  }
  
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
    configProvisioned,
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

function identityMatchesAccount(account: AccountState, draftName: string, draftEmail: string): boolean {
  const requestedIdentity = buildUsername(draftName, draftEmail).toLowerCase();
  if (!requestedIdentity) {
    return true;
  }
  const currentIdentity = buildUsername(account.name || account.email || '', account.email || account.name || '').toLowerCase();
  if (!currentIdentity) {
    return false;
  }
  return requestedIdentity === currentIdentity;
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
  clearSignedOutFlag();

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

    if (!registerResponse.ok) {
      if (registerResponse.status === 409) {
        throw new Error('username is not available (already taken).');
      }
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

async function updateSubscriptionPlan(plan: Plan, options: { unregister?: boolean } = {}): Promise<AccountState | null> {
  const payload = await fetchJSON('/api/auth/subscription', {
    method: 'POST',
    body: JSON.stringify({ plan, unregister: Boolean(options.unregister) }),
  });
  if (payload?.unregistered) {
    return null;
  }
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
  clearSignedOutFlag();
  const response = await fetch('/api/auth/login', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  if (!response.ok) {
    throw new Error('Unable to sign in to your account.');
  }
  const refreshed = await loadAccountFromServer();
  if (!refreshed.isAuthenticated) {
    const fallback = await fetchJSON('/api/auth/me', { method: 'GET' }).catch(() => null);
    if (fallback?.user) {
      return {
        ...refreshed,
        name: fallback.user.username || refreshed.name || username,
        email: fallback.user.email || refreshed.email || '',
        role: normalizeRole(fallback.user.role),
        plan: normalizePlan(fallback.user.subscription_plan),
        billingStatus: normalizeBillingStatus(fallback.user.billing_status),
        trialActive: normalizeBillingStatus(fallback.user.billing_status) === 'active' && normalizePlan(fallback.user.subscription_plan) === 'starter',
        isAuthenticated: true,
        isSystemAdmin: Boolean(fallback.user.can_access_data_manager) || isSystemAdminRole(fallback.user.role),
        checkoutUrl: fallback.user.billing?.checkout_url || '',
        lastWebhookEvent: fallback.user.billing?.last_webhook_event || '',
        failureReason: fallback.user.billing?.failure_reason || '',
        lastWebhookAt: fallback.user.billing?.last_webhook_at || '',
        payments: Array.isArray(fallback.user.payments) ? fallback.user.payments : [],
        subscriptions: Array.isArray(fallback.user.subscriptions) ? fallback.user.subscriptions : [],
        configProvisioned: refreshed.configProvisioned,
      };
    }
  }
  return refreshed;
}

async function logoutFromServer(): Promise<void> {
  if (typeof window !== 'undefined') {
    markSignedOut();
    
    // Clear authentication tokens (HQ mode)
    window.localStorage.removeItem('sop_access_token');
    window.localStorage.removeItem('sop_refresh_token');
    window.localStorage.removeItem('sop_session_token');
    window.localStorage.removeItem('access_token');
    window.localStorage.removeItem('sop_userId');
    window.sessionStorage.removeItem('sop_access_token');
    window.sessionStorage.removeItem('sop_refresh_token');
    window.sessionStorage.removeItem('sop_session_token');
    window.sessionStorage.removeItem('access_token');
    window.sessionStorage.removeItem('sop_userId');
    
    // Clear daemon-specific tokens to prevent state corruption after mode switching
    window.localStorage.removeItem('sop_daemon_access_token');
    window.localStorage.removeItem('sop_daemon_refresh_token');
    window.localStorage.removeItem('sop_daemon_token_expiry');
    window.localStorage.removeItem('sop_chat_runtime');
    
    // Clear session-specific state
    window.localStorage.removeItem('sop_last_subscription_check_timestamp');
    
    // Preserve user preferences and configuration:
    // - sop_daemon_url (custom daemon port)
    // - sop_theme_mode (theme preference)
    // - LLM API keys and provider settings
    // - Other non-auth configuration
  }
  clearBrowserSessionCookies();
  await fetch('/api/auth/logout', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
  });
}

/**
 * Open Data Manager UI in daemon mode (connects to local daemon instead of HQ).
 * 
 * This function is triggered by the "AI Studio" button and:
 * 1. Checks for custom daemon URL in localStorage
 * 2. Builds URL with ?mode=daemon query parameter
 * 3. Adds &daemon_url= if custom port configured
 * 4. Opens Data Manager in new tab
 * 
 * The Data Manager UI will:
 * - Auto-bootstrap authentication with daemon
 * - Use site session token for daemon auth
 * - Connect all API calls to daemon (not HQ)
 * 
 * CRITICAL: Do NOT change DEFAULT_DAEMON_URL without updating:
 * - lib/sop/tools/sopbackend/ui_routes.go (DefaultDaemonURL)
 * - lib/sop/tools/sopbackend/templates/scripts_daemon.html (DEFAULT_DAEMON_URL)
 * - lib/sop/tools/sopbackend/templates/scripts_part01.html (DEFAULT_DAEMON_URL)
 * - lib/sop/tools/sopbackend/templates/scripts_part05.html (DEFAULT_DAEMON_URL)
 * 
 * See: docs/AI_STUDIO_DAEMON_MODE.md for complete specification
 */
async function openLocalWorkflow(event: MouseEvent<HTMLAnchorElement>): Promise<void> {
  event.preventDefault();
  
  // Clear HQ tokens to prevent conflicts when switching to daemon mode
  clearAIStudioHQTokens();
  
  // Check if user has configured a custom daemon URL (stored in localStorage)
  const customDaemonURL = window.localStorage.getItem('sop_daemon_url');
  
  // Build relative URL to go through site backend first
  let url = '/app?mode=daemon';
  if (customDaemonURL && customDaemonURL.trim() !== '' && customDaemonURL !== DEFAULT_DAEMON_URL) {
    // Pass custom daemon URL as query parameter
    url += `&daemon_url=${encodeURIComponent(customDaemonURL)}`;
  }
  
  // Pass parent site URL so AI Studio can provide a back button
  url += `&site_url=${encodeURIComponent(window.location.origin)}`;
  url += `&site_label=${encodeURIComponent('Back to Dashboard')}`;
  
  // Open Data Manager UI in daemon mode - it will connect to daemon instead of HQ
  // The Data Manager UI has daemon authentication built in (scripts_daemon.html)
  // User can also change daemon URL from within Data Manager settings
  window.open(url, '_blank', 'noopener,noreferrer');
}

function ThemeToggle({ theme, onToggle }: { theme: ThemeMode; onToggle: () => void }) {
  const label = theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode';
  return (
    <button
      type="button"
      className="theme-toggle"
      onClick={onToggle}
      aria-label={label}
      title={label}
    >
      {theme === 'dark' ? (
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" viewBox="0 0 16 16">
          <path d="M8 11a3 3 0 1 1 0-6 3 3 0 0 1 0 6zm0 1a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM8 0a.5.5 0 0 1 .5.5v2a.5.5 0 0 1-1 0v-2A.5.5 0 0 1 8 0zm0 13a.5.5 0 0 1 .5.5v2a.5.5 0 0 1-1 0v-2A.5.5 0 0 1 8 13zm8-5a.5.5 0 0 1-.5.5h-2a.5.5 0 0 1 0-1h2a.5.5 0 0 1 .5.5zM3 8a.5.5 0 0 1-.5.5h-2a.5.5 0 0 1 0-1h2A.5.5 0 0 1 3 8zm10.657-5.657a.5.5 0 0 1 0 .707l-1.414 1.415a.5.5 0 1 1-.707-.708l1.414-1.414a.5.5 0 0 1 .707 0zm-9.193 9.193a.5.5 0 0 1 0 .707L3.05 13.657a.5.5 0 0 1-.707-.707l1.414-1.414a.5.5 0 0 1 .707 0zm9.193 2.121a.5.5 0 0 1-.707 0l-1.414-1.414a.5.5 0 0 1 .707-.707l1.414 1.414a.5.5 0 0 1 0 .707zM4.464 4.465a.5.5 0 0 1-.707 0L2.343 3.05a.5.5 0 1 1 .707-.707l1.414 1.414a.5.5 0 0 1 0 .708z"/>
        </svg>
      ) : (
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" viewBox="0 0 16 16">
          <path d="M6 .278a.768.768 0 0 1 .08.858 7.208 7.208 0 0 0-.878 3.46c0 4.021 3.278 7.277 7.318 7.277.527 0 1.04-.055 1.533-.16a.787.787 0 0 1 .81.316.733.733 0 0 1-.031.893A8.349 8.349 0 0 1 8.344 16C3.734 16 0 12.286 0 7.71 0 4.266 2.114 1.312 5.124.06A.752.752 0 0 1 6 .278zM4.858 1.311A7.269 7.269 0 0 0 1.025 7.71c0 4.02 3.279 7.276 7.319 7.276a7.316 7.316 0 0 0 5.205-2.162c-.337.042-.68.063-1.029.063-4.61 0-8.343-3.714-8.343-8.29 0-1.167.242-2.278.681-3.286z"/>
        </svg>
      )}
    </button>
  );
}

export default function App() {
  const [account, setAccount] = useState<AccountState>(emptyState);
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [showInstallStep, setShowInstallStep] = useState(false);
  const [view, setView] = useState<ViewMode>(() => resolveView(typeof window === 'undefined' ? '/' : window.location.pathname));
  const [checkoutComplete, setCheckoutComplete] = useState(false);
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => resolveThemePreference());
  const [expandedImportDetails, setExpandedImportDetails] = useState(false);
  const [showProductPreview, setShowProductPreview] = useState(false);
  const [statusMessage, setStatusMessage] = useState('');
  const [isStatusError, setIsStatusError] = useState(false);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isRefreshingBilling, setIsRefreshingBilling] = useState(false);
  const [provisioningDone, setProvisioningDone] = useState(false);
  const isRefreshingRef = useRef(false);

  useEffect(() => {
    applyThemePreference(themeMode);
    if (typeof window !== 'undefined') {
      try {
        window.localStorage.setItem(THEME_STORAGE_KEY, themeMode);
      } catch {
        // Ignore storage failures when theme persistence is unavailable.
      }
    }
  }, [themeMode]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    const syncFromSystemPreference = () => {
      try {
        const storedTheme = window.localStorage.getItem(THEME_STORAGE_KEY);
        if (storedTheme === 'light' || storedTheme === 'dark') {
          return;
        }
      } catch {
        // Ignore storage failures during sync.
      }
      setThemeMode(mediaQuery.matches ? 'dark' : 'light');
    };
    const handleStorage = (event: StorageEvent) => {
      if (event.key !== THEME_STORAGE_KEY) {
        return;
      }
      if (event.newValue === 'light' || event.newValue === 'dark') {
        setThemeMode(event.newValue);
      } else {
        syncFromSystemPreference();
      }
    };
    syncFromSystemPreference();
    if (typeof mediaQuery.addEventListener === 'function') {
      mediaQuery.addEventListener('change', syncFromSystemPreference);
    } else {
      mediaQuery.addListener(syncFromSystemPreference);
    }
    window.addEventListener('storage', handleStorage);
    return () => {
      if (typeof mediaQuery.removeEventListener === 'function') {
        mediaQuery.removeEventListener('change', syncFromSystemPreference);
      } else {
        mediaQuery.removeListener(syncFromSystemPreference);
      }
      window.removeEventListener('storage', handleStorage);
    };
  }, []);

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
          clearSignedOutFlag();
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

  // Refresh account state when returning to home page (e.g., after login redirect)
  useEffect(() => {
    if (typeof window === 'undefined' || view !== 'home') {
      return;
    }
    // Only refresh if not already authenticated (gap after login redirect)
    if (!account.isAuthenticated) {
      void (async () => {
        try {
          const refreshedAccount = await loadAccountFromServer();
          if (refreshedAccount.isAuthenticated) {
            console.info('auth refresh detected authenticated session after redirect');
            setAccount({
              ...refreshedAccount,
              name: refreshedAccount.name || name,
              email: refreshedAccount.email || email,
            });
            setName(refreshedAccount.name || name);
            setEmail(refreshedAccount.email || email);
          }
        } catch (error) {
          console.warn('auth refresh failed', error);
        }
      })();
    }
  }, [view]);

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
    setIsStatusError(false);
    try {
      const serverAccount = await loadAccountFromServer();
      console.log('[handleSubscription] server account:', serverAccount);
      const resolvedAuthAccount = mergeAccountWithFallback(serverAccount, account, name, email);
      console.log('[handleSubscription] resolved auth account:', resolvedAuthAccount);
      console.log('[handleSubscription] form name:', name.trim(), 'form email:', email.trim());
      const draftName = name.trim();
      const draftEmail = email.trim();
      const explicitIdentityProvided = Boolean(draftName || draftEmail);
      const shouldSwitchToExplicitIdentity = Boolean(password.trim()) && (!resolvedAuthAccount.isAuthenticated || (explicitIdentityProvided && !identityMatchesAccount(resolvedAuthAccount, draftName, draftEmail)));

      if (!resolvedAuthAccount.isAuthenticated && !password.trim()) {
        throw new Error('Please choose a password to continue.');
      }
      if (!resolvedAuthAccount.isAuthenticated && password.trim() !== confirmPassword.trim()) {
        throw new Error('Your chosen passwords do not match.');
      }
      const token = await getHumanProofChallenge();
      if (shouldSwitchToExplicitIdentity) {
        try {
          await logoutFromServer();
        } catch {
          // Ignore logout failures; the subsequent login/registration will establish the intended identity.
        }
      }
      const nextAccount = shouldSwitchToExplicitIdentity || !resolvedAuthAccount.isAuthenticated
        ? await ensureAuthenticatedAccount(account.name || draftName, account.email || draftEmail, password.trim(), token)
        : resolvedAuthAccount;
      if (nextAccount.isAuthenticated) {
        clearSignedOutFlag();
      }
      if (!nextAccount.isAuthenticated) {
        throw new Error('Please sign in or create an account before continuing.');
      }
      const effectiveName = nextAccount.name || account.name || name.trim();
      const effectiveEmail = nextAccount.email || account.email || email.trim();
      console.log('[handleSubscription] calling createCheckoutSession');
      const checkout = await createCheckoutSession('starter', token, effectiveName, effectiveEmail);
      console.log('[handleSubscription] checkout session created:', checkout);
      if (checkout.url) {
        persistPendingCheckoutSessionID(checkout.id);
        const pendingAccount: AccountState = {
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
        };
        setAccount(pendingAccount);
        persistAccountState(pendingAccount);
        setName(pendingAccount.name || draftName);
        setEmail(pendingAccount.email || draftEmail);
        clearSignedOutFlag();
        setShowInstallStep(false);
        setCheckoutComplete(false);
        setStatusMessage('We are sending you to checkout. Your starter plan will activate after payment confirmation.');
        window.location.assign(checkout.url);
        return;
      }
      const synced = await updateSubscriptionPlan('starter');
      if (!synced) {
        throw new Error('Your account is no longer available to update.');
      }
      if (synced.billingStatus !== 'pending') {
        clearPendingCheckoutSessionID();
      }
      const activeAccount: AccountState = {
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
      };
      setAccount(activeAccount);
      persistAccountState(activeAccount);
      setShowInstallStep(synced.billingStatus === 'active');
      setCheckoutComplete(synced.billingStatus === 'active');
      setStatusMessage(
        synced.billingStatus === 'active'
          ? 'Subscription confirmed. Your starter plan is now active.'
          : 'Your starter subscription is pending confirmation.'
      );
    } catch (error) {
      console.error('[handleSubscription] Error caught:', error);
      const errorMessage = error instanceof Error ? error.message : 'Unable to complete the subscription.';
      console.error('[handleSubscription] Setting error message:', errorMessage);
      setStatusMessage(errorMessage);
      setIsStatusError(true);
      if (typeof window !== 'undefined' && errorMessage.toLowerCase().includes('already taken')) {
        window.alert('That username is already taken. Please choose a different username.');
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleUnsubscribe = async () => {
    if (typeof window !== 'undefined') {
      const confirmed = window.confirm('Cancel your Northstar subscription?');
      if (!confirmed) {
        setStatusMessage('Subscription cancellation was skipped.');
        setIsStatusError(false);
        return;
      }
    }

    const shouldRemoveAccount = typeof window !== 'undefined'
      ? window.confirm('Also remove your account from the site and sign you out?')
      : false;

    setIsSubmitting(true);
    setStatusMessage('');
    setIsStatusError(false);
    try {
      const synced = await updateSubscriptionPlan('none', { unregister: shouldRemoveAccount });
      clearPendingCheckoutSessionID();
      if (shouldRemoveAccount) {
        setAccount(emptyState);
        clearPersistedAccountState();
        if (typeof window !== 'undefined') {
          window.localStorage.removeItem(DRAFT_STORAGE_KEY);
          window.location.assign('/site/');
        }
        setName('');
        setEmail('');
        setPassword('');
        setConfirmPassword('');
        setCheckoutComplete(false);
        setShowInstallStep(false);
        setStatusMessage('Your subscription was canceled and your account was removed from the site.');
        setIsStatusError(false);
        return;
      }
      if (!synced) {
        throw new Error('Your account is no longer available to update.');
      }
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
      setIsStatusError(false);
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to unsubscribe right now.');
      setIsStatusError(true);
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleUnregister = async () => {
    setIsSubmitting(true);
    setStatusMessage('');
    setIsStatusError(false);
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
      setIsStatusError(false);
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to remove the account.');
      setIsStatusError(true);
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleDownload = async () => {
    setIsSubmitting(true);
    setStatusMessage('');
    setIsStatusError(false);
    try {
      await downloadLocalAgent();
      setStatusMessage('Opening the local-agent download link.');
      setIsStatusError(false);
    } catch (error) {
      setStatusMessage(error instanceof Error ? error.message : 'Unable to start the download right now.');
      setIsStatusError(true);
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
      const draftName = name.trim();
      const draftEmail = email.trim();
      const explicitIdentityProvided = Boolean(draftName || draftEmail);
      const providedPassword = password.trim();
      let resolvedAccount = mergeAccountWithFallback(await loadAccountFromServer(), account, draftName, draftEmail);

      if (!resolvedAccount.isAuthenticated && explicitIdentityProvided && providedPassword) {
        const reauthenticated = await loginWithCredentials(buildUsername(draftName, draftEmail), providedPassword);
        resolvedAccount = mergeAccountWithFallback(reauthenticated, resolvedAccount, draftName, draftEmail);
      }

      if (!resolvedAccount.isAuthenticated && account.isAuthenticated) {
        resolvedAccount = {
          ...resolvedAccount,
          ...account,
          isAuthenticated: true,
        };
      }

      if (resolvedAccount.billingStatus === 'pending') {
        await pingPendingCheckout();
        const refreshedAccount = await loadAccountFromServer();
        resolvedAccount = mergeAccountWithFallback(refreshedAccount, resolvedAccount, draftName, draftEmail);
      }

      if (pendingCheckoutSessionID) {
        const recoveredAccount = await recoverPendingCheckout(pendingCheckoutSessionID);
        if (recoveredAccount) {
          resolvedAccount = mergeAccountWithFallback(recoveredAccount, resolvedAccount, draftName, draftEmail);
        }
      }

      setAccount(resolvedAccount);
      setName(resolvedAccount.name || draftName);
      setEmail(resolvedAccount.email || draftEmail);
      setShowInstallStep(resolvedAccount.billingStatus === 'active' && resolvedAccount.plan === 'starter');
      setCheckoutComplete(resolvedAccount.billingStatus === 'active' && resolvedAccount.plan === 'starter');
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
  const hasSubscription = Boolean(account.isAuthenticated && account.plan !== 'none');
  const canAccessLocalDaemon = Boolean(account.isAuthenticated) && !account.isSystemAdmin && account.plan !== 'none';
  // If backend config not provisioned, primary action should launch setup wizard
  const needsSetup = account.isAuthenticated && !account.configProvisioned;
  const dataManagerHref = `/app?site_url=${encodeURIComponent(window.location.origin)}&site_label=${encodeURIComponent('Back to Dashboard')}`;
  const primaryActionHref = needsSetup ? dataManagerHref : (canLaunchDataManager ? dataManagerHref : '/site/subscribe');
  const handleOpenDataManager = async (event?: MouseEvent<HTMLAnchorElement>) => {
    if (event) {
      event.preventDefault();
    }
    if (typeof window === 'undefined') {
      return;
    }
    
    // Clear daemon tokens to prevent conflicts when switching to HQ mode
    clearAIStudioDaemonTokens();
    
    try {
      await fetchJSON('/api/auth/status', { credentials: 'include' });
    } catch {
      // Fall back to the direct navigation below so the app still opens even if the auth probe is transiently unavailable.
    }
    
    // Build URL with parent site navigation parameters
    let url = '/app';
    url += `?site_url=${encodeURIComponent(window.location.origin)}`;
    url += `&site_label=${encodeURIComponent('Back to Dashboard')}`;
    
    window.location.assign(url);
  };
  const primaryActionLabel = needsSetup ? 'Setup AI Studio' : (canLaunchDataManager ? 'Open AI Studio' : (hasSubscription ? 'Unsubscribe' : 'Subscribe'));
  
  const secondaryActionHref = canLaunchDataManager ? dataManagerHref : '/site/subscribe';
  const secondaryActionLabel = canLaunchDataManager ? 'Open AI Studio' : hasSubscription ? 'Manage subscription' : 'Review subscription';
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
  const toggleTheme = () => setThemeMode((currentTheme) => (currentTheme === 'dark' ? 'light' : 'dark'));
  const renderGreenEarthBadge = (inline = false) => (
    <div className="page-toolbar-left">
      <a
        className={`green-earth-badge${inline ? ' green-earth-badge-inline' : ''}`}
        href="/docs/green-earth-tech"
        aria-label="Green earth technology"
      >
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M12 2a10 10 0 1 0 10 10A10.01 10.01 0 0 0 12 2Zm0 2a7.99 7.99 0 0 1 6.97 4.03A8.38 8.38 0 0 0 14.5 8.5c-1.2 0-2.3.34-3.27.94A6.96 6.96 0 0 1 12 4Zm-3.8 3.3a6.97 6.97 0 0 1 2.5-.8c.4 0 .79.04 1.17.12a8.53 8.53 0 0 0-1.87 2.2A6.97 6.97 0 0 1 8.2 7.3Zm-1.4 2.74A6.98 6.98 0 0 1 11 6.03c.2.43.36.89.46 1.37A9.2 9.2 0 0 0 7.8 10.04Zm10.79 3.8a8.25 8.25 0 0 1-2.97 2.57 6.95 6.95 0 0 1-4.5-.53c.76-.81 1.3-1.78 1.59-2.87a10.05 10.05 0 0 0 4.24 1.07c.56 0 1.1-.05 1.64-.24Zm-2.76 4.12a7.94 7.94 0 0 1-5.57-.95 8.35 8.35 0 0 0 2.47-1.7c.74-.77 1.24-1.77 1.56-2.9a8.95 8.95 0 0 1 1.54 5.55Z" />
          <path d="M6.5 14.5a7.93 7.93 0 0 0 4.1 2.2 9.42 9.42 0 0 0-.7-2.1 8.16 8.16 0 0 0-3.4-.1Z" />
        </svg>
        <div>
          <strong>Green Earth Tech</strong>
          <span>Local-first AI</span>
        </div>
      </a>
    </div>
  );
  const renderPageShell = (content: JSX.Element, inlineBadge = false) => (
    <main className="app-shell">
      <div className="page-toolbar">
        {renderGreenEarthBadge(inlineBadge)}
        <ThemeToggle theme={themeMode} onToggle={toggleTheme} />
      </div>
      {content}
    </main>
  );

  if (view === 'subscribe') {
    return renderPageShell(
      <section className="hero-card subscription-shell">
          <div className="hero-actions">
            <a className="button button-secondary" href="/site/">Back to overview</a>
            <a className="button button-secondary" href="/site/docs">Product docs</a>
          </div>

          <div className="checkout-grid">
            <div className="checkout-card">
              <p className="eyebrow">Secure checkout</p>
              <h2>Start your Northstar AI Copilot access</h2>
              <p className="muted-copy">
                The offer is simple: one access path to the Northstar AI Copilot experience, with Stores and Spaces that make it easier to organize work, inspect how the AI handled a repository, and review multi-turn workflows. It also includes prebuilt Coding Best Practices for five languages — Golang, C#, Rust, Java, and Python — so you can jump straight into a richer Copilot workflow from day one.
              </p>
              <p className="muted-copy" style={{ marginTop: '0.75rem' }}>
                Your first month is free. After that, billing starts at $5/month and you can cancel anytime.
              </p>
              <p className="muted-copy" style={{ marginTop: '0.75rem' }}>
                The subscription also includes the AI Studio, where you can inspect workflows, review knowledge base activity, and use built-in visualization and studio-style controls for managing data, knowledge bases, and workspace context.
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
                {!account.isAuthenticated ? (
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
                ) : (
                  <div className="preview-box" style={{ padding: '0.85rem 1rem' }}>
                    <strong>Signed in as {account.name || 'your account'}</strong>
                    <p className="muted-copy" style={{ marginTop: '0.3rem' }}>
                      We will use your existing account details and take you straight to Stripe checkout.
                    </p>
                  </div>
                )}
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
                <button
                  type="button"
                  className="button button-primary"
                  onClick={() => { if (hasSubscription) { void handleUnsubscribe(); } else { void handleSubscription(); } }}
                  disabled={isSubmitting}
                >
                  {isSubmitting ? 'Working…' : hasSubscription ? 'Unsubscribe' : 'Subscribe with Stripe'}
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

              {statusMessage ? (
                <p className="muted-copy" style={{ color: isStatusError ? '#e74c3c' : undefined, fontWeight: isStatusError ? '500' : undefined }}>
                  {statusMessage}
                </p>
              ) : null}

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
                  <p>Download the local agent from the official Northstar releases page to run the host-side workflow on your own machine. The agent pairs with the AI copilot so you can move from setup to guided execution.</p>
                  <button type="button" className="button button-secondary" onClick={handleDownload} disabled={isSubmitting}>
                    {isSubmitting ? 'Preparing…' : 'Download local agent'}
                  </button>
                </div>
              ) : null}
            </div>
          </div>
        </section>
    );
  }

  if (view === 'success') {
    return renderPageShell(
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
                <button
                  type="button"
                  className="button button-primary"
                  onClick={() => {
                    if (typeof window !== 'undefined') {
                      window.location.assign('/site/');
                    }
                  }}
                >
                  Continue to dashboard
                </button>
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
    );
  }

  if (view === 'cancel') {
    return renderPageShell(
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
    );
  }

  if (view === 'docs') {
    return renderPageShell(
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
              The experience also includes a live Expertise Dashboard to show which knowledge bases influenced each session, plus one-click refinement controls so you can revalidate with stricter standards or switch knowledge bases instantly.
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
                <li>Semantic category trees organize millions of 'thoughts'(e.g. - code snippets when coding) for instant intelligent search</li>
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

          <div style={{ marginTop: '1rem', marginBottom: '2rem' }}>
            <div className="feature-card">
              <p className="eyebrow">Knowledge workflow</p>
              <h3>Curate knowledge and bring it into Spaces</h3>
              <p className="muted-copy">
                Organize your knowledge in markdown, turn it into importable JSON, and bring it into Spaces so your AI can work from it directly.
              </p>
            </div>
          </div>

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
                </p>
                <button
                  type="button"
                  className="button button-secondary"
                  style={{ marginTop: '0.75rem', padding: '0.6rem 0.9rem' }}
                  onClick={() => setExpandedImportDetails((value) => !value)}
                >
                  {expandedImportDetails ? 'Hide full content' : 'Show full content'}
                </button>
              </div>
            </div>
            <div className="journey-step">
              <span className="journey-number">4</span>
              <div>
                <h3>Automate with expert AI</h3>
                <p>Turn your AI or LLM into an expert for your domain and use it to generate better code, review work, and automate tasks with deeper context and stronger guidance.</p>
              </div>
            </div>
          </div>

          {expandedImportDetails ? (
            <div className="journey-details" style={{ marginTop: '1rem', marginBottom: '2rem' }}>
              <div className="journey-details-content">
                <p>
                  Use the knowledge-base curation manifest as your guide. It tells your AI assistant how to structure the markdown file so the content is organized, consistent, and ready to import.
                </p>
                <p>
                  In practice, you can ask your AI assistant to follow the instructions similar to <a href="/kb/KB_CURATION_MANIFEST.md" style={{ color: 'var(--accent)', fontWeight: 600 }}>KB_CURATION_MANIFEST.md</a> and create a knowledge-base markdown file for you. Once that file is ready, you can compile it into importable JSON and bring it into Spaces.
                </p>
                <p>Example structure:</p>
                <pre>{`# Platform Foundations

## Architecture

This section explains the system design.

- Item: Filesystem and hybrid backend model
  Summary: The platform supports a filesystem-first approach and a hybrid metadata model.
  Body:
  This setup works well for both small deployments and larger environments.
  Sources: Architecture notes, product docs
`}</pre>
                <p>
                  Keep the content clear, concise, and fact-based, and let the manifest guide the structure so the import process stays simple.
                </p>
              </div>
            </div>
          ) : null}

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
    );
  }

  if (shouldShowDashboard) {
    return renderPageShell(
      <>
        <section className="hero-card">
          <div className="hero-grid">
            <div>
              <p className="eyebrow">Northstar workspace</p>
              <h1 className="hero-title">Welcome back to the new site dashboard</h1>
              <p className="hero-copy">
                Setup is complete. Review your account, keep your subscription current, and use the AI Studio for AI copilot workflows.
              </p>
              <div className="hero-actions">
                <a
                  className="button button-primary"
                  href={primaryActionHref}
                  onClick={(event) => {
                    if (canLaunchDataManager) {
                      void handleOpenDataManager(event);
                    }
                  }}
                >
                  {primaryActionLabel}
                </a>
                {account.isAuthenticated ? (
                  <button type="button" className="button button-secondary" onClick={handleLogout} disabled={isSubmitting}>
                    {isSubmitting ? 'Working…' : 'Log out'}
                  </button>
                ) : null}
                {canAccessLocalDaemon ? (
                  <a className="button button-secondary" href="http://127.0.0.1:9090" target="_blank" rel="noreferrer" onClick={(e) => void openLocalWorkflow(e)}>AI Studio</a>
                ) : null}
                <a className="button button-secondary" href="/site/docs">Product docs</a>
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
                  Continue with local-agent setup, review your subscription, or use the AI Studio for AI copilot workflows.
                </p>
                <div className="action-row">
                  <a
                    className="button button-secondary"
                    href={secondaryActionHref}
                    onClick={(event) => {
                      if (canLaunchDataManager) {
                        void handleOpenDataManager(event);
                      }
                    }}
                  >
                    {secondaryActionLabel}
                  </a>
                  <a className="button button-secondary" href="/docs/">Documentation</a>
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
                    <span className="summary-label">AI Studio for HQ</span>
                    {canLaunchDataManager ? (
                      <a href={dataManagerHref} className="summary-link" onClick={(event) => { void handleOpenDataManager(event); }}>Open AI Studio</a>
                    ) : (
                      <strong>Admin access required</strong>
                    )}
                  </div>
                </div>
              </div>
            </div>
          </section>
        ) : null}
      </>
    );
  }

  return renderPageShell(
      <section className="hero-card">
        <div className="hero-grid">
          <div className="hero-content">
            <p className="eyebrow">Northstar</p>
            <h1 className="hero-title">A privacy first modern AI copilot powered by knowledge bases</h1>
            <p className="hero-copy" style={{ marginTop: '-1rem' }}>
              Unlike current AI copilots that store your conversation history on their servers and run overnight optimization in data centers, Northstar keeps everything on your device with real-time intelligence. Bring your own API keys for direct control. Load knowledge bases to turn your LLM into a domain expert, get higher-quality code, and manage your data locally with built-in visualization. We don't store your chat history—our site only handles auth.
            </p>
            <p className="hero-copy" style={{ marginTop: '-1rem' }}>
              <strong>Privacy-first fleet learning with PII detection and governance.</strong> Your daemon learns locally either way. Optionally share patterns with automatic PII detection, privacy governance, and full auditable transparency—see exactly what's shared with per-category opt-in/opt-out controls.
            </p>
            <p className="hero-copy" style={{ marginTop: '-1rem' }}>
              <strong>Full AI copilot features, zero data center processing.</strong> Other copilots need overnight K-Means clustering at HQ. Ours uses semantic category trees that work instantly on your machine.
            </p>
            <p className="hero-copy" style={{ marginTop: '-1rem' }}>
              <strong>Built-in expertise validation saves you time and money.</strong> Unlike other copilots that require manual refinement cycles, our automated KB Quality system validates code against best practices during generation—no additional prompts, no costly back-and-forth iterations. Expertise is enforced automatically, cutting context usage by up to 70%.
            </p>
            <div className="hero-actions" style={{ marginTop: '-1rem' }}>
              <a 
                className="button button-primary" 
                href={primaryActionHref}
                onClick={(event) => {
                  if (needsSetup || canLaunchDataManager) {
                    void handleOpenDataManager(event);
                  }
                }}
              >
                {primaryActionLabel}
              </a>
              <a className="button button-secondary" href="/site/docs">Product docs</a>
              <a className="button button-secondary" href="/docs/fleet-learning">Fleet Learning</a>
              <a className="button button-secondary" href="/docs/sdk/intro">Coming soon</a>
            </div>

            <div className="summary-panel hero-summary">
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
                      <a
                        className="button button-secondary"
                        href="/login?return=/site/"
                      >
                        Log in
                      </a>
                    ) : null}
                    {account.isAuthenticated ? (
                      <button type="button" className="button button-secondary" onClick={handleLogout} disabled={isSubmitting}>
                        Log out
                      </button>
                    ) : null}
                    {canAccessLocalDaemon ? (
                      <a className="button button-secondary" href={DEFAULT_DAEMON_URL} target="_blank" rel="noreferrer" onClick={(e) => void openLocalWorkflow(e)}>AI Studio</a>
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

              {statusMessage ? (
                <p className="muted-copy" style={{ marginTop: '0.75rem', color: isStatusError ? '#e74c3c' : undefined, fontWeight: isStatusError ? '500' : undefined }}>
                  {statusMessage}
                </p>
              ) : null}

              {shouldShowInstallPanel ? (
                <div className="preview-box install-box">
                  <h4>Next step: install the local agent</h4>
                  <p>Your AI Copilot access is active, so the local agent is ready to install. Download it from the official Northstar releases page and pair it with the AI copilot to launch the host-side workflow.</p>
                  <button type="button" className="button button-secondary" onClick={handleDownload} disabled={isSubmitting}>
                    {isSubmitting ? 'Preparing…' : 'Download local agent'}
                  </button>
                </div>
              ) : null}
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
                <li>Semantic category trees organize millions of 'thoughts'(e.g. - code snippets when coding) for instant intelligent search</li>
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

        <div className="preview-toggle-shell" style={{ marginTop: '1.25rem' }}>
          <button
            type="button"
            className="preview-toggle"
            onClick={() => setShowProductPreview((value) => !value)}
            aria-expanded={showProductPreview}
          >
            <span>
              <strong>See the AI Copilot and the AI Studio in action</strong>
              <small>A standard copilot, but with spaces, visualization, and shared context.</small>
            </span>
            <span className="preview-toggle-icon">{showProductPreview ? '▾' : '▸'}</span>
          </button>
          {showProductPreview ? (
            <div className="preview-demo-shell" role="region" aria-label="AI Studio preview">
              <div className="preview-demo-frame">
                <div className="preview-demo-sidebar">
                  <div className="preview-demo-chip active">Spaces</div>
                  <div className="preview-demo-chip">Knowledge</div>
                  <div className="preview-demo-chip">Graph</div>
                </div>
                <div className="preview-demo-main">
                  <div className="preview-demo-toolbar">
                    <span className="preview-demo-pill">Northstar workspace</span>
                    <span className="preview-demo-pill muted">Realtime context</span>
                  </div>
                  <div className="preview-demo-canvas">
                    <div className="preview-demo-card preview-demo-card-large">
                      <p className="preview-demo-label">Active Space</p>
                      <h5>Product Engineering</h5>
                      <div className="preview-demo-graph" />
                    </div>
                    <div className="preview-demo-card preview-demo-card-small">
                      <p className="preview-demo-label">Copilot</p>
                      <p>“Summarize the latest architecture decisions and highlight risks.”</p>
                      <div className="preview-demo-bubble">
                        <strong>Context aware answer</strong>
                        <span>Visualized memory, linked evidence, and local data context.</span>
                      </div>
                    </div>
                  </div>
                </div>
              </div>
              <p className="preview-demo-caption">
                This is a standard AI copilot experience, elevated with local data visualization, knowledge bases, and trusted context.
              </p>
            </div>
          ) : null}
        </div>
      </section>
    );
}
