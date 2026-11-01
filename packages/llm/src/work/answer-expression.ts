/**
 * Arithmetic for answer what-if controls. The host validates each formula and
 * the browser evaluates it on every control change, so both use this parser.
 * It reads numbers, names, + - * / ^, parentheses and a few functions; it
 * never runs model text as code.
 */

export type Expression =
  | { kind: "number"; value: number }
  | { kind: "name"; name: string }
  | { kind: "negate"; operand: Expression }
  | { kind: "binary"; op: "+" | "-" | "*" | "/" | "^"; left: Expression; right: Expression }
  | { kind: "call"; fn: FunctionName; args: Expression[] };

const FUNCTIONS = {
  min: (args: number[]) => Math.min(...args),
  max: (args: number[]) => Math.max(...args),
  abs: (args: number[]) => Math.abs(args[0]!),
  floor: (args: number[]) => Math.floor(args[0]!),
  ceil: (args: number[]) => Math.ceil(args[0]!),
  round: (args: number[]) => {
    const factor = 10 ** (args[1] ?? 0);
    return Math.round(args[0]! * factor) / factor;
  },
} as const;

type FunctionName = keyof typeof FUNCTIONS;

const ARITY: Record<FunctionName, [number, number]> = {
  min: [1, 8], max: [1, 8], abs: [1, 1], floor: [1, 1], ceil: [1, 1], round: [1, 2],
};

export const NAME_PATTERN = /^[a-z_][a-z0-9_]*$/;

type Token = { type: "number"; value: number } | { type: "name"; value: string } | { type: "op"; value: string };

function tokenize(source: string): Token[] {
  const tokens: Token[] = [];
  let index = 0;
  while (index < source.length) {
    const char = source[index]!;
    if (/\s/.test(char)) { index += 1; continue; }
    const number = /^(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?/.exec(source.slice(index));
    if (number) {
      tokens.push({ type: "number", value: Number(number[0]) });
      index += number[0].length;
      continue;
    }
    const name = /^[A-Za-z_][A-Za-z0-9_]*/.exec(source.slice(index));
    if (name) {
      tokens.push({ type: "name", value: name[0].toLowerCase() });
      index += name[0].length;
      continue;
    }
    if ("+-*/^(),".includes(char)) {
      tokens.push({ type: "op", value: char });
      index += 1;
      continue;
    }
    throw new Error(`unexpected "${char}"`);
  }
  return tokens;
}

/** Parse a formula, or throw an Error that names the problem. */
export function parseExpression(source: string): Expression {
  const tokens = tokenize(source);
  let position = 0;
  const peek = () => tokens[position];
  const takeOp = (value: string) => {
    const token = peek();
    if (token?.type === "op" && token.value === value) { position += 1; return true; }
    return false;
  };

  const primary = (): Expression => {
    const token = tokens[position++];
    if (!token) throw new Error("the formula ends early");
    if (token.type === "number") return { kind: "number", value: token.value };
    if (token.type === "name") {
      if (takeOp("(")) {
        if (!(token.value in FUNCTIONS)) throw new Error(`unknown function ${token.value}`);
        const fn = token.value as FunctionName;
        const args: Expression[] = [];
        if (!takeOp(")")) {
          do args.push(additive()); while (takeOp(","));
          if (!takeOp(")")) throw new Error(`close ${fn}( with )`);
        }
        const [least, most] = ARITY[fn];
        if (args.length < least || args.length > most) throw new Error(`${fn} takes ${least === most ? least : `${least} to ${most}`} values`);
        return { kind: "call", fn, args };
      }
      return { kind: "name", name: token.value };
    }
    if (token.value === "(") {
      const inner = additive();
      if (!takeOp(")")) throw new Error("close ( with )");
      return inner;
    }
    if (token.value === "-") return { kind: "negate", operand: power() };
    if (token.value === "+") return power();
    throw new Error(`unexpected "${token.value}"`);
  };
  const power = (): Expression => {
    const base = primary();
    return takeOp("^") ? { kind: "binary", op: "^", left: base, right: power() } : base;
  };
  const multiplicative = (): Expression => {
    let left = power();
    for (;;) {
      if (takeOp("*")) left = { kind: "binary", op: "*", left, right: power() };
      else if (takeOp("/")) left = { kind: "binary", op: "/", left, right: power() };
      else return left;
    }
  };
  const additive = (): Expression => {
    let left = multiplicative();
    for (;;) {
      if (takeOp("+")) left = { kind: "binary", op: "+", left, right: multiplicative() };
      else if (takeOp("-")) left = { kind: "binary", op: "-", left, right: multiplicative() };
      else return left;
    }
  };

  if (tokens.length === 0) throw new Error("the formula is empty");
  const expression = additive();
  if (position < tokens.length) throw new Error(`unexpected "${String(tokens[position]!.value)}"`);
  return expression;
}

/** Every name the formula reads. */
export function expressionNames(expression: Expression, into = new Set<string>()): Set<string> {
  if (expression.kind === "name") into.add(expression.name);
  else if (expression.kind === "negate") expressionNames(expression.operand, into);
  else if (expression.kind === "binary") { expressionNames(expression.left, into); expressionNames(expression.right, into); }
  else if (expression.kind === "call") for (const arg of expression.args) expressionNames(arg, into);
  return into;
}

export function evaluateExpression(expression: Expression, scope: Readonly<Record<string, number>>): number {
  switch (expression.kind) {
    case "number": return expression.value;
    case "name": return scope[expression.name] ?? Number.NaN;
    case "negate": return -evaluateExpression(expression.operand, scope);
    case "call": return FUNCTIONS[expression.fn](expression.args.map((arg) => evaluateExpression(arg, scope)));
    case "binary": {
      const left = evaluateExpression(expression.left, scope);
      const right = evaluateExpression(expression.right, scope);
      switch (expression.op) {
        case "+": return left + right;
        case "-": return left - right;
        case "*": return left * right;
        case "/": return left / right;
        case "^": return left ** right;
      }
    }
  }
}
