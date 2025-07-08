"""
Daemon manager class for managing subprocess daemons.

DaemonManager starts a subprocess that runs a specified command as a daemon.
"""
import subprocess
import threading
import time
import signal
import os
from typing import Optional
import logging

logger = logging.getLogger(__name__)


class ProcessManager:
    """
    A class that manages a daemon subprocess.
    
    When created, starts a daemon in a subprocess.
    When the object instance is destroyed, closes the daemon gracefully.
    """
    
    def __init__(self, command: Optional[list[str]] = None):
        """
        Initialize the daemon manager.
        
        Args:
            command: Command to run as daemon. Defaults to ['sleep', 'infinity']
        """
        self.command = command or ['sleep', 'infinity']
        self.process: Optional[subprocess.Popen] = None
        self.timeout: float = 5.0  # Default timeout for operations
        self._shutdown_event = threading.Event()
        self._monitor_thread: Optional[threading.Thread] = None
        
        # Start the daemon
        self._start_daemon()
        
    def _start_daemon(self):
        """Start the daemon subprocess."""
        try:
            logger.info("Starting daemon with command: %r", self.command)

            # Start the subprocess
            self.process = subprocess.Popen(
                self.command,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                stdin=subprocess.PIPE,
                preexec_fn=os.setsid  # Create new process group
            )
            
            # Start monitoring thread
            self._monitor_thread = threading.Thread(
                target=self._monitor_process,
                daemon=True
            )
            self._monitor_thread.start()

            logger.info("Daemon started with PID: %d", self.process.pid)

        except Exception as e:
            logger.error("Failed to start daemon: %s", e)
            raise

    def _monitor_process(self):
        """Monitor the daemon process and handle unexpected exits."""
        while not self._shutdown_event.is_set():
            if self.process and self.process.poll() is not None:
                # Process has exited
                if not self._shutdown_event.is_set():
                    logger.warning("Daemon process %d exited unexpectedly", self.process.pid)
                break
            time.sleep(0.1)

    def is_running(self) -> bool:
        """Check if the daemon is currently running."""
        return (
            self.process is not None 
            and self.process.poll() is None 
            and not self._shutdown_event.is_set()
        )
    
    def get_pid(self) -> Optional[int]:
        """Get the PID of the daemon process."""
        return self.process.pid if self.process else None
    
    def __call__(self, *args, **kwargs) -> str:
        """
        Invoke the daemon by sending arguments to stdin and returning stdout.
        
        Args:
            *args: Positional arguments to send to stdin
            **kwargs: Keyword arguments to send to stdin
            
        Returns:
            str: The stdout response from the daemon
            
        Raises:
            RuntimeError: If daemon is not running
            TimeoutError: If operation times out
        """
        if not self.is_running():
            raise RuntimeError("Daemon is not running")
        
        # Prepare input data
        if args or kwargs:
            # Convert arguments to a string format
            input_data = []
            for arg in args:
                input_data.append(str(arg))
            for key, value in kwargs.items():
                input_data.append(f"{key}={value}")
            input_str = "\n".join(input_data) + "\n"
        else:
            input_str = "\n"
        
        try:
            # Send input to stdin and read from stdout
            self.process.stdin.write(input_str.encode('utf-8'))
            self.process.stdin.flush()

            # Read response from stdout (with timeout)
            import select
            ready, _, _ = select.select([self.process.stdout], [], [], self.timeout)

            if ready:
                output = self.process.stdout.readline().decode('utf-8').strip()
                return output
            else:
                raise TimeoutError("Daemon did not respond within timeout")

        except Exception as e:
            raise RuntimeError(f"Error communicating with daemon: {e}")

    def send_input(self, data: str) -> str:
        """
        Send raw string data to daemon stdin and return stdout response.
        
        Args:
            data: String data to send to daemon
            
        Returns:
            str: The stdout response from the daemon
        """
        if not self.is_running():
            raise RuntimeError("Daemon is not running")
            
        try:
            self.process.stdin.write(data.encode('utf-8'))
            self.process.stdin.flush()
            
            # Read response with timeout
            import select
            ready, _, _ = select.select([self.process.stdout], [], [], 5.0)
            
            if ready:
                output = self.process.stdout.readline().decode('utf-8').strip()
                return output
            else:
                raise TimeoutError("Daemon did not respond within timeout")
                
        except Exception as e:
            raise RuntimeError(f"Error communicating with daemon: {e}")
    
    def stop_daemon(self):
        """Stop the daemon gracefully."""
        if not self.process or self._shutdown_event.is_set():
            return
            
        logger.info(f"Stopping daemon with PID: {self.process.pid}")
        self._shutdown_event.set()
        
        try:
            # Try graceful shutdown first (SIGTERM)
            os.killpg(os.getpgid(self.process.pid), signal.SIGTERM)
            
            # Wait for graceful shutdown
            try:
                self.process.wait(timeout=5)
                logger.info("Daemon stopped gracefully")
            except subprocess.TimeoutExpired:
                # Force kill if graceful shutdown failed
                logger.warning("Graceful shutdown timed out, force killing daemon")
                os.killpg(os.getpgid(self.process.pid), signal.SIGKILL)
                self.process.wait()
                logger.info("Daemon force killed")

        except ProcessLookupError:
            # Process already dead
            logger.info("Daemon process already terminated")
        except Exception as e:
            logger.error(f"Error stopping daemon: {e}")
        
        # Wait for monitor thread to finish
        if self._monitor_thread and self._monitor_thread.is_alive():
            self._monitor_thread.join(timeout=1)
    
    def __enter__(self):
        """Context manager entry."""
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        """Context manager exit - stop the daemon."""
        self.stop_daemon()
    
    def __del__(self):
        """Destructor - ensure daemon is stopped when object is destroyed."""
        try:
            self.stop_daemon()
        except Exception:
            # Avoid raising exceptions in destructor
            pass


class WasmTimeDaemon(ProcessManager):
    """
    A daemon that runs a WebAssembly (Wasm) time measurement command.
    """

    def __init__(self):
        """Initialize a WasmTime daemon."""
        super().__init__(command=['wasmtime', 'time.wasm'])


# Example usage
if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    
    print("=== Testing basic daemon ===")
    daemon = ProcessManager()
    
    print(f"Daemon running: {daemon.is_running()}")
    print(f"Daemon PID: {daemon.get_pid()}")
    
    # Let it run for a bit
    time.sleep(1)
    
    print("Stopping daemon...")
    daemon.stop_daemon()
    
    print(f"Daemon running: {daemon.is_running()}")
    
    print("\n=== Testing interactive daemon ===")
    # Test with echo daemon (cat command)
    echo_daemon = EchoDaemon()
    
    try:
        print(f"Echo daemon running: {echo_daemon.is_running()}")
        print(f"Echo daemon PID: {echo_daemon.get_pid()}")
        
        # Test calling the daemon with arguments
        response1 = echo_daemon("Hello", "World")
        print(f"Response to ('Hello', 'World'): {response1}")
        
        # Test with keyword arguments
        response2 = echo_daemon(message="Test", value=42)
        print(f"Response to (message='Test', value=42): {response2}")
        
        # Test send_input method
        response3 = echo_daemon.send_input("Direct input test\n")
        print(f"Response to direct input: {response3}")
        
    except Exception as e:
        print(f"Error testing echo daemon: {e}")
    finally:
        echo_daemon.stop_daemon()
    
    print("\n=== Testing Python interpreter daemon ===")
    # Test with Python interpreter
    try:
        python_daemon = InteractiveDaemon(['python3', '-u', '-i'])
        
        print(f"Python daemon running: {python_daemon.is_running()}")
        
        # Send Python commands
        result1 = python_daemon.send_input("print(2 + 2)\n")
        print(f"Python result for '2 + 2': {result1}")
        
        result2 = python_daemon.send_input("import math; print(math.pi)\n")
        print(f"Python result for 'math.pi': {result2}")
        
    except Exception as e:
        print(f"Error testing Python daemon: {e}")
    finally:
        if 'python_daemon' in locals():
            python_daemon.stop_daemon()
