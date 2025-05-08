import type { TwitchSession } from '$lib/types/auth';
import { buildApiUrl } from '$lib/utils';

export const authState = $state({
  loggedIn: false,
  session: null as TwitchSession | null
});

export function resetAuthState() {
  authState.loggedIn = false;
  authState.session = null;
}

export function checkLoginStatus() {
  fetch(buildApiUrl('/check-session'), {
    method: 'GET',
    credentials: 'include' // Important for cookies to be sent with the request
  })
    .then((response) => {
      if (response.ok) {
        return response.json(); // Process the body of the response
      } else {
        throw new Error('Session check failed');
      }
    })
    .then((sessionData) => {
      if (sessionData.services && sessionData.services.length > 0) {
        // logged in
        authState.loggedIn = true;
        authState.session = sessionData as TwitchSession;
      } else {
        resetAuthState();
      }
    })
    .catch((error) => {
      console.error('Error checking login status:', error);
      resetAuthState();
    });
}

export function redirectToTwitchLogin() {
  window.location.href = buildApiUrl('/login/twitch');
}

export function logout() {
  // Correctly handle logout by making a request to the backend endpoint
  fetch(buildApiUrl('/logout'), {
    method: 'POST',
    credentials: 'include' // Important for cookies to be sent with the request
  })
    .then((response) => {
      if (response.ok) {
        localStorage.removeItem('sessionToken'); // Optionally remove from localStorage if used elsewhere
        resetAuthState();
        window.location.href = '/';
      }
    })
    .catch((error) => console.error('Error logging out:', error));
}

export function restartServer() {
  fetch(buildApiUrl('/restart-server'), { method: 'POST' })
    .then((response) => response.json())
    .then((data) => console.log(data))
    .catch((error) => console.error('Error:', error));
}
