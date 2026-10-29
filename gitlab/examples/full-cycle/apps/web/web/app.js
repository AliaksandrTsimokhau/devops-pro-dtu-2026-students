const status = document.querySelector('#status');
const button = document.querySelector('#refresh');
const errorBox = document.querySelector('#error');
const pipeline = document.querySelector('#pipeline');

async function refresh() {
  button.disabled = true;
  status.textContent = 'Checking...';
  status.className = 'status';
  errorBox.hidden = true;
  try {
    const response = await fetch('/api/release', { cache: 'no-store', signal: AbortSignal.timeout(5000) });
    if (!response.ok) throw new Error(`Release API returned HTTP ${response.status}.`);
    const release = await response.json();
    if (!release.version) throw new Error('The release API is missing its version.');
    for (const field of ['version', 'environment', 'release', 'hostname']) {
      document.getElementById(field).textContent = release[field] || 'Local development';
    }
    pipeline.hidden = true;
    if (release.pipelineUrl) {
      const url = new URL(release.pipelineUrl);
      if (url.protocol === 'https:' || url.protocol === 'http:') {
        pipeline.href = url.href;
        pipeline.hidden = false;
      }
    }
    status.textContent = 'Online';
    status.className = 'status online';
    document.querySelector('#checked').textContent = `Verified at ${new Date().toLocaleTimeString()}`;
  } catch (error) {
    status.textContent = 'Unavailable';
    status.className = 'status offline';
    errorBox.textContent = `${error.message} Check the deployment and try again.`;
    errorBox.hidden = false;
    document.querySelector('#checked').textContent = 'Showing the last successful release, if available.';
  } finally {
    button.disabled = false;
  }
}

button.addEventListener('click', refresh);
refresh();
