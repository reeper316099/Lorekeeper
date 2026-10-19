// electron-builder configuration. The product name comes from the single APP_NAME constant.
const fs = require('node:fs');
const path = require('node:path');

const constants = fs.readFileSync(
  path.join(__dirname, '..', '..', 'packages', 'shared', 'src', 'constants.ts'),
  'utf8',
);
const APP_NAME = /APP_NAME = '([^']+)'/.exec(constants)?.[1] ?? 'Lorekeeper';
const slug = APP_NAME.toLowerCase().replace(/[^a-z0-9]+/g, '-');

/** @type {import('electron-builder').Configuration} */
module.exports = {
  appId: `app.${slug}.desktop`,
  productName: APP_NAME,
  // The package name is scoped (@lorekeeper/desktop), which is not a legal file name on Linux.
  executableName: slug,
  copyright: `Copyright © ${new Date().getFullYear()} ${APP_NAME} contributors`,
  directories: { output: 'release', buildResources: 'build' },
  files: ['dist/**/*', '!dist/**/*.map', 'package.json'],
  asar: true,
  artifactName: `${slug}-\${version}-\${os}-\${arch}.\${ext}`,
  // Builds are unsigned for now: see docs/DISTRIBUTION.md for the signing hooks to add later.
  mac: {
    category: 'public.app-category.entertainment',
    target: [{ target: 'dmg', arch: ['universal'] }],
    identity: null,
    hardenedRuntime: false,
  },
  win: {
    target: [
      { target: 'nsis', arch: ['x64'] },
      { target: 'portable', arch: ['x64'] },
    ],
  },
  nsis: { oneClick: false, allowToChangeInstallationDirectory: true },
  linux: {
    target: ['AppImage', 'deb', 'rpm'],
    category: 'Utility',
    executableName: slug,
    desktop: {
      entry: { Name: APP_NAME, Comment: 'Track what you read and watch', StartupWMClass: APP_NAME },
    },
    maintainer: `${APP_NAME} contributors <noreply@users.noreply.github.com>`,
  },
};
