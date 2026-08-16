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
        
        // Update user profile across UI
        updateUserProfile(data.user);
        
        if (typeof onSuccess === 'function') {
            onSuccess(data.user);
        }
    } catch (err) {
        console.error('Session validation error:', err);
        window.location.href = 'index.html';
    }
}

// Update user profile display names & avatars dynamically
function updateUserProfile(user) {
    const name = (user && user.name) ? user.name : 'Mohammed Nazeer Ali';
    const email = (user && user.username) ? user.username : 'nazeer@nazeerdfs.io';

    document.querySelectorAll('.user-name-display, #sidebarUserName, #headerUserName, #profileName, #propOwner').forEach(el => {
        el.textContent = name;
    });

    document.querySelectorAll('.user-email-display, #profileEmail').forEach(el => {
        el.value = email;
        if (el.tagName !== 'INPUT') el.textContent = email;
    });

    // Update avatar seeds
    document.querySelectorAll('img[src*="api.dicebear.com"]').forEach(img => {
        img.src = 'https://api.dicebear.com/7.x/notionists/svg?seed=MohammedNazeer';
    });
}

// Fetch and apply live real-time storage metrics across all pages
async function loadRealtimeStorageMetrics() {
    try {
        const res = await fetch('/api/dashboard');
        if (!res.ok) return;
        const data = await res.json();

        const usedFormatted = data.usedStorageFormatted || '0 B';
        const usedPercent = data.storageUsedPercent || 0;
        const totalVolume = data.totalVolumeFormatted || '400 GB';
        const activeNodes = data.activeNodesCount || 3;

        // Update live metrics on elements
        document.querySelectorAll('.live-used-storage').forEach(el => {
            el.textContent = usedFormatted;
        });

        document.querySelectorAll('.live-total-storage').forEach(el => {
            el.textContent = totalVolume;
        });

        document.querySelectorAll('.live-storage-percent').forEach(el => {
            el.textContent = `${usedPercent}%`;
        });

        document.querySelectorAll('.live-storage-bar').forEach(el => {
            el.style.width = `${Math.max(usedPercent, 2)}%`;
        });

        document.querySelectorAll('.live-active-nodes').forEach(el => {
            el.textContent = `${activeNodes} Nodes Active`;
        });
    } catch (err) {
        console.warn('Realtime metrics update warning:', err);
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

// Setup common logout listeners and live metrics ticker
function bindLogoutButtons() {
    document.querySelectorAll('#logoutBtn, a[href="#logout"]').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            logoutUser();
        });
    });

    // Initial real-time storage sync
    loadRealtimeStorageMetrics();
    // Poll real-time storage metrics every 10 seconds
    setInterval(loadRealtimeStorageMetrics, 10000);
}

document.addEventListener('DOMContentLoaded', bindLogoutButtons);
