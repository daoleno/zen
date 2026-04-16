const fs = require('fs');
const path = require('path');
const { expo: baseConfig } = require('./app.base.json');

loadEnvFile(path.join(__dirname, '.env.local'));

const projectId = typeof process.env.ZEN_EXPO_PROJECT_ID === 'string'
  ? process.env.ZEN_EXPO_PROJECT_ID.trim()
  : '';

module.exports = () => {
  const extra = { ...(baseConfig.extra || {}) };

  if (projectId) {
    extra.eas = { projectId };
  }

  return {
    ...baseConfig,
    extra,
  };
};

function loadEnvFile(filePath) {
  if (!fs.existsSync(filePath)) {
    return;
  }

  const content = fs.readFileSync(filePath, 'utf8');
  for (const rawLine of content.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) {
      continue;
    }

    const separatorIndex = line.indexOf('=');
    if (separatorIndex <= 0) {
      continue;
    }

    const key = line.slice(0, separatorIndex).trim();
    if (!key || Object.prototype.hasOwnProperty.call(process.env, key)) {
      continue;
    }

    let value = line.slice(separatorIndex + 1).trim();
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1);
    }

    process.env[key] = value;
  }
}
