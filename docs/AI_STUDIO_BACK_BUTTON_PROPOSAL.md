# AI Studio: Back to Site Navigation - Proposal

## Overview

Add a clickable button/icon in the AI Studio UI that allows users to navigate back to the parent site's dashboard/main page. This should work generically for any application that launches the AI Studio (sopbackend UI).

## Current State

### How AI Studio is Launched

**From Site Frontend** (`src/frontend/src/App.tsx`):
```javascript
// "AI Studio" button opens Data Manager in daemon mode
<a className="button button-secondary" 
   href="http://127.0.0.1:9090" 
   onClick={(e) => void openLocalWorkflow(e)}>
   AI Studio
</a>

// openLocalWorkflow function:
async function openLocalWorkflow(event) {
  event.preventDefault();
  let url = '/app?mode=daemon';
  if (customDaemonURL) {
    url += `&daemon_url=${encodeURIComponent(customDaemonURL)}`;
  }
  window.open(url, '_blank', 'noopener,noreferrer');
}
```

**Current Query Parameters**:
- `?mode=daemon` - Connect to local daemon instead of HQ
- `&daemon_url=<url>` - Custom daemon URL (optional)

### Template Data Structure

**Backend** (`lib/sop/tools/sopbackend/ui_routes.go`):
```go
func (r *UIRouter) landingPageData(req *http.Request) map[string]any {
    // ... existing fields ...
    return map[string]any{
        "Version":                 sop.Version,
        "Username":                username,
        "DaemonURL":               daemonURL,
        "DaemonRuntimeEnabled":    daemonRuntimeEnabled,
        // ... other fields ...
    }
}
```

## Proposed Solution

### 1. Add Generic Site URL Field

**Backend Change** (`lib/sop/tools/sopbackend/ui_routes.go`):

```go
func (r *UIRouter) landingPageData(req *http.Request) map[string]any {
    // ... existing code ...
    
    // ========================================================================
    // PARENT SITE NAVIGATION
    // ========================================================================
    // Allow parent application to specify its landing page URL so users can
    // navigate back from AI Studio to the main site dashboard.
    //
    // Usage: /app?mode=daemon&site_url=http://localhost:8080
    //
    // This makes sopbackend generic so any app launching AI Studio can
    // provide its own return URL without hardcoding.
    // ========================================================================
    parentSiteURL := ""
    parentSiteLabel := "Back to Site"
    if siteURL := strings.TrimSpace(req.URL.Query().Get("site_url")); siteURL != "" {
        // Validate URL format
        if parsedURL, err := url.Parse(siteURL); err == nil && parsedURL.Scheme != "" {
            parentSiteURL = siteURL
            // Optional: Allow custom label via site_label parameter
            if label := strings.TrimSpace(req.URL.Query().Get("site_label")); label != "" {
                parentSiteLabel = label
            }
        }
    }
    
    return map[string]any{
        // ... existing fields ...
        "ParentSiteURL":   parentSiteURL,
        "ParentSiteLabel": parentSiteLabel,
    }
}
```

**Frontend Change** (`src/frontend/src/App.tsx`):

```typescript
async function openLocalWorkflow(event: MouseEvent<HTMLAnchorElement>): Promise<void> {
  event.preventDefault();
  
  const customDaemonURL = window.localStorage.getItem('sop_daemon_url');
  
  let url = '/app?mode=daemon';
  if (customDaemonURL && customDaemonURL.trim() !== '' && customDaemonURL !== DEFAULT_DAEMON_URL) {
    url += `&daemon_url=${encodeURIComponent(customDaemonURL)}`;
  }
  
  // NEW: Add site URL so AI Studio can show "Back to Dashboard" button
  const siteURL = window.location.origin; // e.g., http://localhost:8080
  url += `&site_url=${encodeURIComponent(siteURL)}`;
  
  // Optional: Custom label
  url += `&site_label=${encodeURIComponent('Back to Dashboard')}`;
  
  window.open(url, '_blank', 'noopener,noreferrer');
}
```

### 2. UI Placement Options

I propose **three potential locations** for the back button. Please review and select your preferred option.

---

## Option A: Top-Right Corner (Recommended)

**Location**: Fixed position in the top-right corner of the page, always visible

**Visual Position**:
```
┌────────────────────────────────────────────────────┐
│  [Sidebar]  │  Main Content              [← Back] │
│             │                                      │
│  Database   │  Select Store or Context Space      │
│  Stores     │                                      │
│  Spaces     │  [Content Area]                     │
│             │                                      │
└────────────────────────────────────────────────────┘
```

**Advantages**:
- ✅ Always visible, doesn't scroll away
- ✅ Standard web convention (top-right = navigation/actions)
- ✅ Doesn't interfere with existing sidebar or content
- ✅ Works well on mobile (fixed header position)
- ✅ Clear visual hierarchy

**Implementation** (`templates/index.html`):

```html
<body>
{{if .DaemonRuntimeEnabled}}
<script>
(function() {
    try {
        localStorage.setItem('sop_chat_runtime', 'local');
        localStorage.setItem('sop_daemon_url', '{{.DaemonURL}}');
        window.SOP_DAEMON_BASE_URL = '{{.DaemonURL}}';
    } catch (err) {
        console.debug('[daemon-ui] failed to initialize daemon runtime state', err);
    }
})();
</script>
{{end}}

{{if .ParentSiteURL}}
<!-- Back to Site Button - Fixed Top Right -->
<a href="{{.ParentSiteURL}}" 
   class="back-to-site-btn" 
   title="{{.ParentSiteLabel}}"
   target="_self">
    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" viewBox="0 0 16 16">
        <path fill-rule="evenodd" d="M15 8a.5.5 0 0 0-.5-.5H2.707l3.147-3.146a.5.5 0 1 0-.708-.708l-4 4a.5.5 0 0 0 0 .708l4 4a.5.5 0 0 0 .708-.708L2.707 8.5H14.5A.5.5 0 0 0 15 8z"/>
    </svg>
    <span>{{.ParentSiteLabel}}</span>
</a>
{{end}}

{{template "sidebar" .}}
<!-- rest of content -->
```

**Styles** (`templates/styles.html`):

```css
.back-to-site-btn {
    position: fixed;
    top: 12px;
    right: 12px;
    z-index: 1000;
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 14px;
    background-color: var(--success-bg);
    color: white;
    border-radius: 6px;
    text-decoration: none;
    font-size: 0.9em;
    font-weight: 500;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
    transition: all 0.2s ease;
}

.back-to-site-btn:hover {
    background-color: var(--success-hover);
    transform: translateY(-1px);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
}

.back-to-site-btn:active {
    transform: translateY(0);
    box-shadow: 0 2px 6px rgba(0, 0, 0, 0.15);
}

.back-to-site-btn svg {
    flex-shrink: 0;
}

/* Responsive: show icon only on small screens */
@media (max-width: 768px) {
    .back-to-site-btn span {
        display: none;
    }
    .back-to-site-btn {
        padding: 8px;
    }
}
```

---

## Option B: Sidebar Header

**Location**: At the top of the sidebar, above the Database section

**Visual Position**:
```
┌────────────────────────────────────────────────────┐
│ [← Back]    │  Main Content                        │
│─────────────│                                      │
│  Database   │  Select Store or Context Space      │
│  Stores     │                                      │
│  Spaces     │  [Content Area]                     │
│             │                                      │
└────────────────────────────────────────────────────┘
```

**Advantages**:
- ✅ Logical position (sidebar = navigation)
- ✅ Doesn't require fixed positioning
- ✅ Groups with other navigation elements
- ✅ Scrolls with sidebar content

**Disadvantages**:
- ⚠️ Less prominent than top-right
- ⚠️ Can scroll out of view if sidebar is long

**Implementation** (`templates/sidebar.html`):

```html
{{define "sidebar"}}
<div id="sidebar">
    {{if .ParentSiteURL}}
    <!-- Back to Site Link - Sidebar Header -->
    <div style="padding: 8px 12px; margin-bottom: 12px; border-bottom: 1px solid var(--border-color);">
        <a href="{{.ParentSiteURL}}" 
           class="sidebar-back-link" 
           title="{{.ParentSiteLabel}}">
            <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" fill="currentColor" viewBox="0 0 16 16">
                <path fill-rule="evenodd" d="M15 8a.5.5 0 0 0-.5-.5H2.707l3.147-3.146a.5.5 0 1 0-.708-.708l-4 4a.5.5 0 0 0 0 .708l4 4a.5.5 0 0 0 .708-.708L2.707 8.5H14.5A.5.5 0 0 0 15 8z"/>
            </svg>
            <span>{{.ParentSiteLabel}}</span>
        </a>
    </div>
    {{end}}
    
    <div style="flex: 1; overflow-y: auto;">
        <!-- existing database section -->
```

**Styles**:

```css
.sidebar-back-link {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-color);
    text-decoration: none;
    font-size: 0.9em;
    padding: 6px 8px;
    border-radius: 4px;
    transition: background-color 0.2s ease;
}

.sidebar-back-link:hover {
    background-color: var(--hover-bg);
    color: var(--success-bg);
}

.sidebar-back-link svg {
    flex-shrink: 0;
}
```

---

## Option C: Sidebar Footer

**Location**: In the sidebar footer, next to the "Environments" button

**Visual Position**:
```
┌────────────────────────────────────────────────────┐
│  [Sidebar]  │  Main Content                        │
│  Database   │  Select Store or Context Space      │
│  Stores     │                                      │
│  Spaces     │  [Content Area]                     │
│─────────────│                                      │
│ [← Back][⚙]│                                       │
└────────────────────────────────────────────────────┘
```

**Advantages**:
- ✅ Keeps footer area for navigation/settings
- ✅ Doesn't add visual clutter to top
- ✅ Groups with environment settings
- ✅ Always visible (footer is sticky)

**Disadvantages**:
- ⚠️ Less discoverable (users look top-right first)
- ⚠️ May be missed on first glance

**Implementation** (`templates/sidebar.html`):

```html
<!-- Sidebar Footer -->
<div style="padding-top: 10px; border-top: 1px solid var(--border-color); display: flex; justify-content: flex-end; align-items: center; gap: 10px;">
    {{if $.ParentSiteURL}}
    <a href="{{$.ParentSiteURL}}" 
       class="sidebar-footer-btn" 
       title="{{$.ParentSiteLabel}}">
        <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" fill="currentColor" viewBox="0 0 16 16">
            <path fill-rule="evenodd" d="M15 8a.5.5 0 0 0-.5-.5H2.707l3.147-3.146a.5.5 0 1 0-.708-.708l-4 4a.5.5 0 0 0 0 .708l4 4a.5.5 0 0 0 .708-.708L2.707 8.5H14.5A.5.5 0 0 0 15 8z"/>
        </svg>
    </a>
    {{end}}
    
    <button id="env-manager-btn" onclick="showEnvironmentsManager()" title="Manage Environments" style="display: none; background: none; border: none; color: var(--text-muted); cursor: pointer; padding: 4px;">
        <!-- existing environment button SVG -->
```

**Styles**:

```css
.sidebar-footer-btn {
    background: none;
    border: none;
    color: var(--text-muted);
    cursor: pointer;
    padding: 4px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    transition: all 0.2s ease;
    text-decoration: none;
}

.sidebar-footer-btn:hover {
    color: var(--success-bg);
    background-color: var(--hover-bg);
}
```

---

## Recommendation

**I recommend Option A (Top-Right Corner)** for these reasons:

1. **Most Discoverable**: Users expect back/close buttons in top-right
2. **Always Visible**: Fixed position means it's never scrolled away
3. **Prominent**: Stands out visually without cluttering the layout
4. **Standard Convention**: Matches web app patterns (Gmail, GitHub, etc.)
5. **Mobile-Friendly**: Works well on all screen sizes with responsive text hiding

---

## Testing Scenarios

### Test Case 1: Site Launch
```
1. User on site dashboard (http://localhost:8080)
2. Click "AI Studio" button
3. AI Studio opens in new tab (/app?mode=daemon&site_url=http://localhost:8080)
4. Back button appears in top-right corner
5. Click back button → returns to http://localhost:8080
```

### Test Case 2: Direct Access
```
1. User navigates directly to /app?mode=daemon
2. No site_url parameter present
3. Back button does NOT appear (no parent site)
```

### Test Case 3: Custom Label
```
1. Site passes site_label parameter
2. /app?mode=daemon&site_url=...&site_label=Back%20to%20Dashboard
3. Button displays "Back to Dashboard" instead of "Back to Site"
```

### Test Case 4: HQ Mode
```
1. User on /app (HQ mode, no mode=daemon parameter)
2. site_url can still be passed if needed
3. Back button works for both HQ and daemon modes
```

---

## Security Considerations

1. **URL Validation**: Only allow http:// and https:// schemes
2. **XSS Prevention**: Template engine auto-escapes URLs
3. **Open Redirect**: Consider whitelist of allowed domains (optional)

```go
// Optional: Whitelist allowed domains
var allowedSiteDomains = []string{
    "localhost",
    "127.0.0.1",
    "yourdomain.com",
}

func isAllowedSiteURL(siteURL string) bool {
    parsed, err := url.Parse(siteURL)
    if err != nil {
        return false
    }
    // Only allow http/https
    if parsed.Scheme != "http" && parsed.Scheme != "https" {
        return false
    }
    // Check domain whitelist (optional)
    for _, domain := range allowedSiteDomains {
        if strings.Contains(parsed.Host, domain) {
            return true
        }
    }
    return false
}
```

---

## Implementation Checklist

- [ ] Add `ParentSiteURL` and `ParentSiteLabel` to `landingPageData()`
- [ ] Parse `site_url` and `site_label` query parameters
- [ ] Validate URL format (http/https scheme required)
- [ ] Update `openLocalWorkflow()` in App.tsx to pass site URL
- [ ] Add back button HTML to chosen template location
- [ ] Add CSS styles for back button
- [ ] Test with site launch
- [ ] Test direct /app access (no button should appear)
- [ ] Test custom labels
- [ ] Test mobile responsiveness
- [ ] Update documentation

---

## Questions for Review

1. **Which UI location do you prefer?** (Option A, B, or C)
2. **Should we validate against a domain whitelist?** (security vs flexibility)
3. **Default button text**: "Back to Site", "Back to Dashboard", or something else?
4. **Icon style**: Left arrow (←) as shown, or home icon (🏠)?
5. **Target behavior**: Open in same tab (`_self`) or same window but replace AI Studio?

---

**Please review the three options and let me know which location you prefer. I'm ready to implement once you've selected your preferred approach.**
