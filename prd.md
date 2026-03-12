# SunaVPN Telegram Bot --- Product Requirements Document (PRD)

## 1. Product Overview

**Product Name:** SunaVPN\
**Type:** Telegram bot for VPN access management\
**VPN Stack:** Xray + VLESS + REALITY\
**Language:** Go (Golang)

The system allows users to obtain and manage VPN access through a
Telegram bot.\
The bot automatically manages users, VPN keys, subscriptions, and server
configuration.

Primary goals:

-   Fully automated VPN access distribution
-   Subscription-based VPN service
-   Minimal manual server management
-   Scalable architecture for future growth

------------------------------------------------------------------------

# 2. System Architecture

The system consists of the following components.

## 2.1 Telegram Bot

Responsible for:

-   User registration
-   VPN key generation
-   Profile management
-   Balance management
-   Subscription management

Core commands:

    /start
    /profile

------------------------------------------------------------------------

## 2.2 Database (SQLite)

Stores persistent user data.

### Users Table

  Field         Description
  ------------- -----------------------------------
  id            internal id
  telegram_id   telegram user id
  uuid          vpn client uuid
  balance       user balance
  devices       allowed device count
  sub_until     subscription expiration timestamp
  referrer_id   referral id
  created_at    user registration time

------------------------------------------------------------------------

## 2.3 Xray Manager

Manages the VPN server configuration.

Responsibilities:

-   Add new client to Xray config
-   Remove client from Xray config
-   Reload Xray service

------------------------------------------------------------------------

## 2.4 VPN Key Generator

Generates connection keys in VLESS format.

Example:

    vless://UUID@SERVER_IP:443?...reality parameters...

------------------------------------------------------------------------

# 3. Current Issues (Must Be Fixed)

## 3.1 Unsafe JSON Manipulation

Xray configuration is currently modified using:

    map[string]interface{}

Problems:

-   Type errors
-   Broken JSON
-   Corrupted configuration
-   Xray crashes

### Required Solution

Introduce strict structures for configuration parsing.

Example:

``` go
type XrayConfig struct {
    Inbounds []Inbound
}
```

------------------------------------------------------------------------

## 3.2 No Transaction Safety

Current flow:

1.  Create user in database
2.  Add user to Xray config

If step 2 fails:

User exists in database but not in VPN server.

### Required Solution

Implement atomic operations:

    Database transaction
    Add user to Xray
    Commit
    Rollback on error

------------------------------------------------------------------------

## 3.3 Missing Config Validation

Before restarting Xray, configuration must be validated.

Required validation command:

    xray -test -config config.json

------------------------------------------------------------------------

## 3.4 Race Conditions

Simultaneous operations may cause configuration corruption.

Example:

Two users created simultaneously.

Required solution:

-   Mutex lock
-   File lock

------------------------------------------------------------------------

## 3.5 Insufficient Logging

The system currently logs only errors.

Required logging:

-   user registered
-   vpn key generated
-   xray client added
-   subscription activated
-   balance updated
-   subscription expired

------------------------------------------------------------------------

# 4. Functional Requirements

## 4.1 User Registration

Command:

    /start

Bot should:

-   create user in database
-   generate UUID
-   assign trial balance (15 rubles)
-   generate VPN key

------------------------------------------------------------------------

## 4.2 User Profile

Command:

    /profile

Bot displays:

-   User ID
-   Balance
-   Devices allowed
-   Subscription expiration
-   Remaining days
-   VPN key

------------------------------------------------------------------------

## 4.3 VPN Key Generation

Keys must follow this format:

    vless://UUID@SERVER_IP:443?encryption=none&security=reality...

All users share the same:

-   server IP
-   public key
-   shortID
-   SNI

Only UUID differs.

------------------------------------------------------------------------

## 4.4 Subscription System

Pricing model:

    5 rubles = 1 day of VPN

Balance should automatically convert to subscription time.

Example:

    balance: 15
    subscription: 3 days

------------------------------------------------------------------------

## 4.5 Device Limits

Field:

    devices

Defines how many devices may use the VPN key simultaneously.

------------------------------------------------------------------------

# 5. Payment System

Supported payment methods:

### 1. Bank Cards

Users can pay using standard bank card payments.

### 2. Telegram Payments

Payments processed directly through Telegram.

No other payment methods are required.

------------------------------------------------------------------------

# 6. Future Features

## 6.1 Referral System

Users receive referral link:

    t.me/bot?start=refID

Rewards:

-   Inviter receives bonus balance
-   New user receives bonus balance

------------------------------------------------------------------------

## 6.2 Automatic User Deactivation

If:

    sub_until < current_time

Bot must automatically:

-   remove user from Xray
-   disable VPN access

------------------------------------------------------------------------

## 6.3 Monitoring System

Bot must periodically verify:

-   Xray process is running
-   Server connectivity

If Xray stops:

Restart service automatically.

------------------------------------------------------------------------

## 6.4 Admin Commands

Admin-only commands:

    /users
    /add_balance
    /ban
    /unban
    /stats

------------------------------------------------------------------------

# 7. Technical Restrictions

IMPORTANT:

Development is performed **on a local machine**, not on the VPN server.

Because of this, execution of server commands is strictly prohibited
during development.

Forbidden commands:

    sudo
    systemctl
    xray
    apt
    bash scripts

These commands must not be executed automatically by the code during
development.

Server management must be handled only after deployment.

------------------------------------------------------------------------

# 8. Security Requirements

Sensitive data must never be stored directly in source code.

Secrets must be stored in environment variables.

Examples:

-   telegram_token
-   server_ip
-   privateKey
-   publicKey

Configuration should be loaded from:

    .env

------------------------------------------------------------------------

# 9. Scalability Requirements

Initial system must support:

    10–20 users

Architecture should allow future scaling up to:

    500+ users

------------------------------------------------------------------------

# 10. Development Roadmap

## Phase 1 --- MVP

-   Telegram bot
-   User registration
-   VPN key generation
-   Profile command
-   Add user to Xray

------------------------------------------------------------------------

## Phase 2

-   Subscription logic
-   Automatic balance conversion
-   Expired user removal

------------------------------------------------------------------------

## Phase 3

-   Payments (cards + Telegram)
-   Referral system

------------------------------------------------------------------------

## Phase 4

-   Admin commands
-   Statistics
-   Monitoring

------------------------------------------------------------------------

# 11. Final Goal

Build a fully automated VPN service where:

-   users connect via Telegram
-   subscriptions are automatic
-   VPN access is provisioned instantly
-   server management is fully automated
