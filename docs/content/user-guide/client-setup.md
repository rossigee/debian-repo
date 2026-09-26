---
title: Client Setup
description: Installing packages from debian-repo
---


Guide for machines that need to install packages from the debian-repo repository.

## Prerequisites

- `apt` package manager (Ubuntu, Debian, or derivative)
- HTTP Basic Auth credentials (username/password) from your admin
- Network access to `debs.myorgname.com` (or your custom domain)

## Adding the Repository

### Method 1: Manual (recommended for one or two machines)

1. Create the apt sources file:

```bash
sudo tee /etc/apt/sources.list.d/myorgname.list > /dev/null <<EOF
deb https://username:password@debs.myorgname.com stable main
EOF
```

Replace `username` and `password` with credentials from your admin.

2. Update the package cache:

```bash
sudo apt update
```

3. Install packages:

```bash
sudo apt install package-name
```

### Method 2: Using /etc/apt/auth.conf.d/ (more secure)

Store credentials separately from the sources file:

1. Create the sources file without credentials:

```bash
sudo tee /etc/apt/sources.list.d/myorgname.list > /dev/null <<EOF
deb https://debs.myorgname.com stable main
EOF
```

2. Create the credentials file:

```bash
sudo tee /etc/apt/auth.conf.d/myorgname.conf > /dev/null <<EOF
machine debs.myorgname.com
login username
password yourpassword
EOF

sudo chmod 600 /etc/apt/auth.conf.d/myorgname.conf
```

3. Update and install:

```bash
sudo apt update
sudo apt install package-name
```

### Method 3: Ansible/Terraform/Configuration Management

**Ansible playbook:**

```yaml
- name: Add debian-repo
  hosts: all
  tasks:
    - name: Add GPG key
      ansible.builtin.get_url:
        url: https://debs.myorgname.com/pubkey.gpg
        dest: /etc/apt/keyrings/myorgname.gpg

    - name: Add repository
      ansible.builtin.apt_repository:
        repo: "deb [signed-by=/etc/apt/keyrings/myorgname.gpg] https://debs.myorgname.com stable main"
        filename: myorgname
        state: present

    - name: Store credentials
      ansible.builtin.copy:
        content: |
          machine debs.myorgname.com
          login {{ debian_repo_user }}
          password {{ debian_repo_password }}
        dest: /etc/apt/auth.conf.d/myorgname.conf
        mode: '0600'

    - name: Update cache
      ansible.builtin.apt:
        update_cache: yes
```

**Terraform:**

```hcl
resource "null_resource" "debian_repo" {
  provisioners "remote-exec" {
    inline = [
      "echo 'deb https://debs.myorgname.com stable main' | sudo tee /etc/apt/sources.list.d/myorgname.list",
      "echo 'machine debs.myorgname.com\\nlogin ${var.debian_user}\\npassword ${var.debian_pass}' | sudo tee /etc/apt/auth.conf.d/myorgname.conf",
      "sudo chmod 600 /etc/apt/auth.conf.d/myorgname.conf",
      "sudo apt update"
    ]
  }
}
```

## Verifying the Repository

```bash
# List all packages available
apt-cache search --names-only '.*'

# Show details of a specific package
apt-cache show package-name

# Check which version would be installed
apt-cache policy package-name
```

## Updating Packages

```bash
# Fetch latest package lists
sudo apt update

# Upgrade all packages (including from myorgname.com)
sudo apt upgrade

# Upgrade a specific package
sudo apt install --upgrade package-name
```

## Pinning Package Versions

If you want to stay on a specific version:

```bash
cat > /etc/apt/preferences.d/myorgname.pref <<EOF
Package: package-name
Pin: version 1.0.0
Pin-Priority: 1000
EOF

sudo apt update
sudo apt install package-name=1.0.0
```

## Troubleshooting

### "E: Could not authenticate to the repository"

**Cause:** Wrong credentials or credentials expired.

**Fix:**
1. Verify username and password with your admin
2. Check credentials are correctly formatted in `/etc/apt/sources.list.d/myorgname.list` or `/etc/apt/auth.conf.d/myorgname.conf`
3. Make sure `/etc/apt/auth.conf.d/myorgname.conf` has mode `600` (readable only by root)

```bash
ls -la /etc/apt/auth.conf.d/
```

### "E: Release file for ... is not valid"

**Cause:** GPG signature validation failed.

**Fix:**
1. Ensure the repository GPG key is installed
2. If using signed-by, verify the key path is correct
3. Try clearing apt cache and updating:

```bash
sudo apt clean
sudo apt update
```

### "E: Could not resolve 'debs.myorgname.com'"

**Cause:** DNS resolution failure.

**Fix:**
1. Check network connectivity: `ping debs.myorgname.com`
2. Verify domain name is correct (default: `debs.myorgname.com`)
3. Check firewall allows HTTPS (port 443)

### "E: Package 'package-name' is not available"

**Cause:** Package not found in the repository.

**Fix:**
1. Verify package name is correct: `apt-cache search package-name`
2. Check you're adding the correct suite and component
3. Ask your admin if the package has been uploaded

## Managing Multiple Repositories

If you have both myorgname.com and another repository:

```bash
# myorgname.com
deb https://username:password@debs.myorgname.com stable main

# ubuntu archive
deb http://archive.ubuntu.com/ubuntu jammy main universe

# ppa
deb http://ppa.launchpad.net/someone/ppa/ubuntu jammy main
```

apt will install from the repository with the highest pin priority (by default, all are equal, so first match wins).

## Security Notes

- ✅ Credentials are transmitted over HTTPS only (encrypted in transit)
- ✅ Packages are verified with GPG signatures (signature checking enabled by default)
- ⚠️ Credentials are stored in plaintext on disk (use file permissions to restrict access)
- ⚠️ Never commit credentials to Git or Docker images
- 💡 Consider using `/etc/apt/auth.conf.d/` to separate credentials from sources

## Advanced: Custom Domain

If your admin set up debian-repo on a custom domain:

```bash
deb https://username:password@your-custom-domain.com stable main
```

Replace `your-custom-domain.com` with your actual domain.

## See Also

- **[User Guide](_index.md)** — Overview of all user tasks
- **[Admin Guide — Client Authentication](../admin-guide/authentication.md)** — How admins manage credentials
