// NazeerDFS Enterprise Client App Helpers

// Session verification helper for protected pages
async function checkSession(onSuccess) {
    try {
        const res = await fetch('/api/session');
        if (!res.ok) {
            window.location.href = 'index.html';
            return;
        }
        const data = await res.json();
        if (!data.authenticated) {
            window.location.href = 'index.html';
            return;
        }
        if (typeof onSuccess === 'function') {
            onSuccess(data.user);
        }
    } catch (err) {
        console.error('Session validation error:', err);
        window.location.href = 'index.html';
    }
}

// Redirect to dashboard if already logged in (for login page)
async function checkLoggedInRedirect() {
    try {
        const res = await fetch('/api/session');
        if (res.ok) {
            const data = await res.json();
            if (data.authenticated) {
                window.location.href = 'dashboard.html';
            }
        }
    } catch (err) {
        // Ignored on login page
    }
}

// Logout handler
async function logoutUser() {
    try {
        await fetch('/api/logout', { method: 'POST' });
    } catch (err) {
        console.error('Logout error:', err);
    } finally {
        window.location.href = 'index.html';
    }
}

// Setup common logout listeners across all pages
function bindLogoutButtons() {
    document.querySelectorAll('#logoutBtn, a[href="#logout"]').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            logoutUser();
        });
    });
}

document.addEventListener('DOMContentLoaded', bindLogoutButtons);
