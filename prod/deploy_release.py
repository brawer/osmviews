#!/usr/bin/python3
# SPDX-FileCopyrightText: 2022 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Script for deploying a release to production. Needs to be run
# on Wikimedia Toolforge, not your local machine. Usage:
#
# $ ssh bastion.toolforge.org
# $ become osmviews
# $ python3 prod/deploy_release.py                      # deploy latest version
# $ python3 prod/deploy_release.py --release_tag=0.0.2  # specific version


import argparse
import json
import os
import pathlib
import shutil
import subprocess
import urllib.request


def deploy_release(tag):
    path = download_release(tag)
    run_command(['toolforge-jobs', 'flush'])  # stop all cronjobs
    restart_webserver(path)
    start_builder(path)


def download_release(tag):
    url = 'https://api.github.com/repos/brawer/osmviews/releases/'
    if tag is None or tag is 'latest':
        url += 'latest'
    else:
        url += 'tags/' + tag
    with urllib.request.urlopen(url) as response:
        info = json.loads(response.read())
    tag = info['tag_name']  # 'latest' -> actual tag
    assets = info['assets']
    print('Deploying release', tag)
    path = pathlib.Path('bin', tag)
    tmp_path = pathlib.Path('bin', 'tmp-%s.tmp' % tag)
    shutil.rmtree(tmp_path, ignore_errors=True)
    tmp_path.mkdir(parents=True)
    for asset in assets:
        with urllib.request.urlopen(asset['browser_download_url']) as response:
            filepath = pathlib.PurePath(tmp_path, asset['name'])
            with open(filepath, 'wb') as f:
                shutil.copyfileobj(response, f)
            os.chmod(filepath, 0o0755)
    shutil.rmtree(path, ignore_errors=True)
    tmp_path.rename(path)
    return path


def restart_webserver(path):
    binary = (path / 'webserver').resolve()
    run_command(['webservice', '--backend=gridengine', 'generic',
                 'restart', str(path / 'webserver')])


def start_builder(path):
    run_command([  # run daily at 15:59 UTC
        'toolforge-jobs', 'run', 'builder', '--command',
        '%s --keys=keys/storage-key' % (path/'builder'),
         '--image', 'tf-bullseye-std', '--schedule', '59 15 * * *'])


def run_command(cmd):
    print(' '.join(cmd))
    subprocess.run(cmd, check=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        description='Deploy OSMViews release to production')
    parser.add_argument('--release_tag', default='latest', required=False,
                        help="release tag such as '0.0.2', or 'latest'")
    args = parser.parse_args()
    deploy_release(args.release_tag)
