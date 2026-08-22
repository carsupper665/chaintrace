# Separate blockchain transactions from token transfers

The backend stores both TRON transactions and the TRC20 Transfer events they produce, while transaction graph edges represent individual Transfer events. A transfer is identified within its parent transaction rather than by transaction hash alone, preserving multiple token movements from one contract execution and retaining enough provenance for audit and debugging.
