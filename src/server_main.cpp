// Standalone Yakumo ad hoc server.
//
// This is a small entry point around the server that Yakumo itself uses for
// "Host a session" and `Yakumo --adhoc-server`. It deliberately pulls in only
// the adhoc sources, so it builds without the game, the recompiler or SDL3.
//
// The Railway container runs this behind a small protocol-aware gateway: one
// public TCP port carries both the matchmaking service (27312) and the relay
// (27313), because Railway exposes one TCP proxy per service and the game
// expects the relay on the next port.

#include "adhoc/client.hpp"
#include "adhoc/server.hpp"

#include <atomic>
#include <chrono>
#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <iostream>
#include <string>
#include <thread>

namespace mhp3rd::adhoc {

// server.cpp calls these two helpers from the client translation unit.
std::string format_mac(const Mac &mac) {
    char text[18];
    std::snprintf(text, sizeof(text), "%02x:%02x:%02x:%02x:%02x:%02x", mac[0], mac[1], mac[2], mac[3], mac[4],
        mac[5]);
    return text;
}

void Client::log(const std::string &line, bool print) {
    if (print || std::getenv("YAKUMO_ADHOC_VERBOSE") != nullptr) std::cout << line << std::endl;
}

} // namespace mhp3rd::adhoc

namespace {

std::atomic<bool> stop_requested{false};

void on_stop_signal(int) {
    stop_requested = true;
}

std::uint16_t port_from_environment() {
    const char *text = std::getenv("ADHOC_PORT");
    if (text == nullptr || *text == '\0') return mhp3rd::adhoc::kAdhocctlPort;
    const unsigned long value = std::strtoul(text, nullptr, 10);
    if (value < 1024u || value > 65534u) {
        std::cerr << "ADHOC_PORT must be between 1024 and 65534; using 27312\n";
        return mhp3rd::adhoc::kAdhocctlPort;
    }
    return static_cast<std::uint16_t>(value);
}

} // namespace

int main() {
    using namespace mhp3rd::adhoc;

    ServerConfig config;
    config.adhocctl_port = port_from_environment();
    config.print_events = true;

    Server server;
    if (!server.start(config)) {
        std::cerr << "cannot start the ad hoc server: " << server.status().error << "\n";
        return 1;
    }

    std::signal(SIGINT, on_stop_signal);
    std::signal(SIGTERM, on_stop_signal);

    std::cout << "Yakumo ad hoc server listening on TCP " << config.adhocctl_port << " (matchmaking) and "
              << relay_port_for(config.adhocctl_port) << " (relay)" << std::endl;

    while (!stop_requested) std::this_thread::sleep_for(std::chrono::milliseconds(200));

    std::cout << "stopping the ad hoc server" << std::endl;
    server.stop();
    return 0;
}
