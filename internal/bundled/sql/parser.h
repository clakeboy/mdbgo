/* A Bison parser, made by GNU Bison 2.3.  */

/* Skeleton interface for Bison's Yacc-like parsers in C

   Copyright (C) 1984, 1989, 1990, 2000, 2001, 2002, 2003, 2004, 2005, 2006
   Free Software Foundation, Inc.

   This program is free software; you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation; either version 2, or (at your option)
   any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program; if not, write to the Free Software
   Foundation, Inc., 51 Franklin Street, Fifth Floor,
   Boston, MA 02110-1301, USA.  */

/* As a special exception, you may create a larger work that contains
   part or all of the Bison parser skeleton and distribute that work
   under terms of your choice, so long as that work isn't itself a
   parser generator using the skeleton or a modified version thereof
   as a parser skeleton.  Alternatively, if you modify or redistribute
   the parser skeleton itself, you may (at your option) remove this
   special exception, which will cause the skeleton and the resulting
   Bison output files to be licensed under the GNU General Public
   License without this special exception.

   This special exception was added by the Free Software Foundation in
   version 2.2 of Bison.  */

/* Tokens.  */
#ifndef YYTOKENTYPE
# define YYTOKENTYPE
   /* Put the tokens into the symbol table, so that GDB and other debuggers
      know about them.  */
   enum yytokentype {
     IDENT = 258,
     NAME = 259,
     PATH = 260,
     STRING = 261,
     NUMBER = 262,
     OPENING = 263,
     CLOSING = 264,
     SELECT = 265,
     FROM = 266,
     WHERE = 267,
     CONNECT = 268,
     DISCONNECT = 269,
     TO = 270,
     LIST = 271,
     TABLES = 272,
     AND = 273,
     OR = 274,
     NOT = 275,
     LIMIT = 276,
     COUNT = 277,
     STRPTIME = 278,
     DESCRIBE = 279,
     TABLE = 280,
     TOP = 281,
     PERCENT = 282,
     LTEQ = 283,
     GTEQ = 284,
     NEQ = 285,
     LIKE = 286,
     ILIKE = 287,
     IS = 288,
     NUL = 289,
     GT = 290,
     LT = 291,
     EQ = 292
   };
#endif
/* Tokens.  */
#define IDENT 258
#define NAME 259
#define PATH 260
#define STRING 261
#define NUMBER 262
#define OPENING 263
#define CLOSING 264
#define SELECT 265
#define FROM 266
#define WHERE 267
#define CONNECT 268
#define DISCONNECT 269
#define TO 270
#define LIST 271
#define TABLES 272
#define AND 273
#define OR 274
#define NOT 275
#define LIMIT 276
#define COUNT 277
#define STRPTIME 278
#define DESCRIBE 279
#define TABLE 280
#define TOP 281
#define PERCENT 282
#define LTEQ 283
#define GTEQ 284
#define NEQ 285
#define LIKE 286
#define ILIKE 287
#define IS 288
#define NUL 289
#define GT 290
#define LT 291
#define EQ 292




#if ! defined YYSTYPE && ! defined YYSTYPE_IS_DECLARED
typedef union YYSTYPE
#line 55 "parser.y"
{
	char *name;
	double dval;
	int ival;
}
/* Line 1529 of yacc.c.  */
#line 129 "parser.h"
	YYSTYPE;
# define yystype YYSTYPE /* obsolescent; will be withdrawn */
# define YYSTYPE_IS_DECLARED 1
# define YYSTYPE_IS_TRIVIAL 1
#endif



#if ! defined YYLTYPE && ! defined YYLTYPE_IS_DECLARED
typedef struct YYLTYPE
{
  int first_line;
  int first_column;
  int last_line;
  int last_column;
} YYLTYPE;
# define yyltype YYLTYPE /* obsolescent; will be withdrawn */
# define YYLTYPE_IS_DECLARED 1
# define YYLTYPE_IS_TRIVIAL 1
#endif


