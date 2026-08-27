"""Sample Python module for testing."""

import math


class Calculator:
    """A simple calculator class."""

    def __init__(self, value=0):
        self.value = value

    def add(self, x):
        """Add x to the current value."""
        self.value += x
        return self

    def multiply(self, x):
        """Multiply the current value by x."""
        self.value *= x
        return self


def fibonacci(n):
    """Return the nth Fibonacci number."""
    if n <= 1:
        return n
    a, b = 0, 1
    for _ in range(2, n + 1):
        a, b = b, a + b
    return b


def is_prime(n):
    """Check if n is a prime number."""
    if n < 2:
        return False
    for i in range(2, int(math.sqrt(n)) + 1):
        if n % i == 0:
            return False
    return True
